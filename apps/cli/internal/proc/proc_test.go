package proc

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

// sh runs a shell script and returns how it ended and the lines it wrote,
// each as "<stream> <text>".
func sh(ctx context.Context, c Command, script string) (Result, []string, error) {
	c.Path, c.Args = "/bin/sh", []string{"-c", script}
	var mu sync.Mutex
	var got []string
	res, err := Run(ctx, c, func(stream string, text []byte) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, stream+" "+string(text))
	})
	return res, got, err
}

func code(res Result) int {
	if res.ExitCode == nil {
		return -1
	}
	return *res.ExitCode
}

func TestRunHandsOverLinesAndTheExitCode(t *testing.T) {
	t.Parallel()
	res, got, err := sh(context.Background(), Command{}, `echo one; echo two; printf 'no newline'; exit 3`)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"stdout one", "stdout two", "stdout no newline"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if code(res) != 3 || res.Cancelled || res.TimedOut {
		t.Fatalf("result = %+v (exit %d), want exit 3", res, code(res))
	}

	_, got, err = sh(context.Background(), Command{Env: []string{"WORD=said"}}, `echo "$WORD" >&2`)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"stderr said"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

func TestRunReportsACommandThatCannotStart(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), Command{Path: "/nonexistent/agentx"}, func(string, []byte) {})
	if err == nil {
		t.Fatal("no error for a program that does not exist")
	}
}

// A cancel stops the command and what it started: the group gets SIGTERM,
// which the shell and the sleep it started both die of. Were the shell
// alone signalled, the sleep would hold the output open for the grace
// period. The sleep is started before the shell says it is ready: a shell
// may block signals while it starts a command, and a signal that arrives
// then reaches the shell alone.
func TestRunStopsTheGroupOnCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var once sync.Once
	done := make(chan Result, 1)
	go func() {
		res, _ := Run(ctx, Command{Path: "/bin/sh", Args: []string{"-c", `sleep 60 & echo ready; wait`}},
			func(_ string, text []byte) {
				if string(text) == "ready" {
					once.Do(func() { close(started) })
				}
			})
		done <- res
	}()
	<-started
	begun := time.Now()
	cancel()
	res := <-done
	if !res.Cancelled || res.TimedOut || res.ExitCode != nil {
		t.Fatalf("result = %+v (exit %d), want cancelled and ended by a signal", res, code(res))
	}
	if took := time.Since(begun); took > Grace {
		t.Fatalf("the run took %v to stop: the sleep it started kept its output open", took)
	}
}

// A command that ignores SIGTERM is killed after the grace period, and a
// passed deadline stops a run as a cancel does.
func TestRunKillsAfterTheGracePeriodOnADeadline(t *testing.T) {
	t.Parallel()
	res, _, err := sh(context.Background(), Command{Deadline: 100 * time.Millisecond, Grace: 100 * time.Millisecond},
		`trap '' TERM; while :; do sleep 0.05; done`)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.Cancelled || res.ExitCode != nil {
		t.Fatalf("result = %+v (exit %d), want timed out and killed by a signal", res, code(res))
	}
}
