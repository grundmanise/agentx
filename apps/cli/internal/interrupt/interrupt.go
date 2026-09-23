// Package interrupt turns the signals that ask a run to stop into a stop
// the user can read: the first SIGINT or SIGTERM cancels the run's context,
// so the git child in flight is killed and the command unwinds through its
// own error path with an exit code and a result event; the second restores
// the signal's default and raises it again, so a run that will not stop can
// still be killed exactly as it could before.
//
// Nothing here stops a change that is part way through. A mutation journal,
// and the recovery of one, run on a context taken off this cancellation
// with Uninterruptible, so that a stop lands between the steps of a
// mutation and never inside one. The crash-recovery guarantees stand behind
// that and are not weakened by it: a second signal, a SIGKILL, a power cut
// or a terminal signalling the whole foreground process group still leave a
// journal for the next command, exactly as they did.
package interrupt

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Stop is what a watched run carries: which stop signal reached it, if any.
// A run reads it when it is over, to answer for a stop rather than for
// whatever its cancelled git call happened to report.
type Stop struct {
	signal atomic.Int32 // the first stop signal, 0 until one arrives
}

// Signal is the stop signal the run received, 0 when none did.
func (s *Stop) Signal() syscall.Signal { return syscall.Signal(s.signal.Load()) }

// Interrupted reports whether a stop signal reached the run.
func (s *Stop) Interrupted() bool { return s.signal.Load() != 0 }

type stopKey struct{}

// From returns the Stop the context carries, or nil for a context no Watch
// made, which is every context a test builds for itself.
func From(ctx context.Context) *Stop {
	s, _ := ctx.Value(stopKey{}).(*Stop)
	return s
}

// Interrupted reports whether a stop signal reached the run of ctx.
func Interrupted(ctx context.Context) bool {
	s := From(ctx)
	return s != nil && s.Interrupted()
}

// settle is how long Settle waits for a signal that has been delivered to
// reach the watch. Crossing from the runtime's handler to the watching
// goroutine is a scheduling hop, so the wait is generous; it ends the
// moment the signal arrives and is only ever asked for by a run that has
// reason to expect one.
const settle = 500 * time.Millisecond

// Settle reports whether a stop signal ended the run, waiting a moment for
// one that has been delivered but not yet seen. A run asks for this when it
// is failing and has reason to expect a stop: a terminal signals the whole
// foreground process group, so the git a run was waiting on dies of the
// same Ctrl-C, and its error can reach the run's answer before the signal
// does. Without the wait the run would answer for the git it killed instead
// of for the stop. A context no Watch made never waits.
func Settle(ctx context.Context) bool {
	s := From(ctx)
	if s == nil {
		return false
	}
	if !s.Interrupted() {
		select {
		case <-ctx.Done():
		case <-time.After(settle):
		}
	}
	return s.Interrupted()
}

// Uninterruptible is ctx with cancellation taken off it, for the work a
// stop may not leave half done: the ref steps of a mutation journal, and of
// the recovery a later command runs before its own work. Those reach git
// after the journal that describes them is on disk, where giving up costs
// the next command a recovery and gains nothing — the journal is already
// written, so the cheapest way out is through. A run that is stopping
// before its journal exists still gives up at once, since every other call
// keeps the cancellation.
func Uninterruptible(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

// Watch installs the handler for the stop signals and returns a context
// cancelled by the first of them, carrying the Stop that says so, and a
// function that takes the handler off again. Call it once, from the process
// entry point; a context it did not make is simply never interrupted, which
// is what every test drives.
func Watch(parent context.Context) (context.Context, func()) {
	s := &Stop{}
	ctx, cancel := context.WithCancel(context.WithValue(parent, stopKey{}, s))
	signals := stopSignals()
	if len(signals) == 0 {
		return ctx, cancel
	}
	// Two slots: the signal that stops the run and the one that kills it.
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, signals...)
	quit := make(chan struct{})
	go watch(s, ch, cancel, quit)
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			signal.Stop(ch)
			close(quit)
		})
		cancel()
	}
}

// watch answers the stop signals until the watch is taken off. The first
// one cancels the run; a later one kills the process the way an unhandled
// signal would, which is the way out of a run that does not stop and is
// safe because every durable change is journaled before it starts.
func watch(s *Stop, ch <-chan os.Signal, cancel context.CancelFunc, quit <-chan struct{}) {
	for {
		select {
		case sig := <-ch:
			num, ok := sig.(syscall.Signal)
			if !ok {
				continue
			}
			if s.signal.CompareAndSwap(0, int32(num)) {
				cancel()
				continue
			}
			signal.Reset(num)
			_ = syscall.Kill(syscall.Getpid(), num)
			return
		case <-quit:
			return
		}
	}
}

// stopSignals are the signals a stop is asked with, minus any the process
// started with ignored. A shell sets SIGINT to ignore in a job it runs in
// the background, and catching it there would stop that job on a Ctrl-C
// meant for the foreground one; nohup does the same with SIGHUP. Honouring
// a disposition the process inherited keeps agentx behaving as every other
// command in that pipeline does.
func stopSignals() []os.Signal {
	var sigs []os.Signal
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		if !signal.Ignored(sig) {
			sigs = append(sigs, sig)
		}
	}
	return sigs
}
