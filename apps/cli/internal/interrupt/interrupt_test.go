package interrupt

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// These tests raise real signals at the test process, which is safe only
// while Watch holds the handler: every one of them takes the watch off
// again before it returns, and none of them raises a second signal, which
// is the one that restores the default and kills the process. The
// end-to-end behaviour of that second signal is covered by the CLI tests,
// which have a child process to kill.

// TestASignalCancelsTheRun is the first signal doing its one job: the run's
// context ends, so every git child of the run is killed and the command
// unwinds through its own error path instead of dying where it stands.
func TestASignalCancelsTheRun(t *testing.T) {
	ctx, stop := Watch(context.Background())
	defer stop()

	if Interrupted(ctx) {
		t.Fatal("the run is interrupted before any signal")
	}
	raise(t, syscall.SIGTERM)
	waitDone(t, ctx)
	if !Interrupted(ctx) {
		t.Error("the context is done but the run does not report the stop")
	}
	if got := From(ctx).Signal(); got != syscall.SIGTERM {
		t.Errorf("the stop signal is %v, want SIGTERM", got)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("the context ended with %v, want a cancellation", ctx.Err())
	}
}

// TestUninterruptibleSurvivesTheSignal is what keeps a mutation whole: the
// ref steps of a journal already on disk run on a context the stop does not
// reach, so a signal lands between the steps of a mutation and never inside
// one.
func TestUninterruptibleSurvivesTheSignal(t *testing.T) {
	ctx, stop := Watch(context.Background())
	defer stop()
	journal := Uninterruptible(ctx)

	raise(t, syscall.SIGINT)
	waitDone(t, ctx)
	if err := journal.Err(); err != nil {
		t.Fatalf("the journal's context ended with %v, want it still running", err)
	}
	// It is still the run's context: what it carries is readable through it,
	// so work done on it can still tell that the run is stopping.
	if !Interrupted(journal) {
		t.Error("the journal's context does not carry the stop")
	}
}

// TestAContextNoWatchMadeIsNeverInterrupted is what every test of the CLI
// drives: Run takes a plain context, and nothing about signals leaks into
// it.
func TestAContextNoWatchMadeIsNeverInterrupted(t *testing.T) {
	ctx := context.Background()
	if From(ctx) != nil {
		t.Error("a plain context carries a stop")
	}
	if Interrupted(ctx) {
		t.Error("a plain context reports a stop")
	}
	if Uninterruptible(ctx).Err() != nil {
		t.Error("a plain context is not uninterruptible")
	}
}

// TestAnIgnoredSignalIsLeftIgnored keeps agentx behaving like every other
// command in a script: a shell sets SIGINT to ignore in a job it runs in
// the background, and a run that caught it there would stop on a Ctrl-C
// meant for the job in the foreground.
func TestAnIgnoredSignalIsLeftIgnored(t *testing.T) {
	signal.Ignore(syscall.SIGINT)
	defer signal.Notify(caught, syscall.SIGINT) // back to the net TestMain holds

	for _, sig := range stopSignals() {
		if sig == syscall.SIGINT {
			t.Fatal("SIGINT is watched although the process started with it ignored")
		}
	}
	// SIGTERM is not what a shell ignores, so it is still answered.
	var watched bool
	for _, sig := range stopSignals() {
		watched = watched || sig == syscall.SIGTERM
	}
	if !watched {
		t.Error("SIGTERM is not watched")
	}
}

// TestTakingTheWatchOffEndsTheRun covers the ordinary end of a run: the
// handler goes, and the context the watch made is cancelled so nothing it
// started keeps running.
func TestTakingTheWatchOffEndsTheRun(t *testing.T) {
	ctx, stop := Watch(context.Background())
	stop()
	stop() // taking a watch off twice is not an error
	waitDone(t, ctx)
	if Interrupted(ctx) {
		t.Error("a run nothing signalled reports a stop")
	}
}

// raise sends sig to this process. Every caller holds a watch, so the
// signal is caught and never kills the test binary; the caller then waits
// for the run's context to end, which is how it knows it was answered.
func raise(t *testing.T, sig syscall.Signal) {
	t.Helper()
	if err := syscall.Kill(syscall.Getpid(), sig); err != nil {
		t.Fatal(err)
	}
}

func waitDone(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not end within five seconds of the signal")
	}
}

// caught is the safety net TestMain holds: a signal raised by a test that
// no watch is holding would kill the test binary outright and report
// nothing, so every stop signal is caught for the whole run and such a test
// fails as a timeout instead, under its own name.
var caught = make(chan os.Signal, 4)

func TestMain(m *testing.M) {
	signal.Notify(caught, syscall.SIGINT, syscall.SIGTERM)
	code := m.Run()
	signal.Stop(caught)
	os.Exit(code)
}
