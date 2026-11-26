package serve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// running is one Run on a goroutine, stopped when the test ends.
type running struct {
	t        *testing.T
	requests *Requests
	ctx      context.Context // done when Run is stopped, as a request of a door is
	cancel   context.CancelFunc
	done     chan error
}

func start(t *testing.T, o Options) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{t: t, requests: NewRequests(), ctx: ctx, cancel: cancel, done: make(chan error, 1)}
	o.Requests = r.requests
	if o.Inventory == nil {
		o.Inventory = func(scan.Inventory) {}
	}
	if o.Warn == nil {
		o.Warn = func(string) {}
	}
	if o.Fail == nil {
		o.Fail = func(err error) { t.Errorf("Fail(%v)", err) }
	}
	go func() { r.done <- Run(ctx, o) }()
	t.Cleanup(func() {
		cancel()
		<-r.done
	})
	return r
}

// refresh asks for a scan and returns what it answered.
func (r *running) refresh() (int, error) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
	defer cancel()
	return r.requests.Refresh(ctx)
}

// stop cancels Run and fails the test unless it returns at once.
func (r *running) stop() {
	r.t.Helper()
	r.cancel()
	select {
	case err := <-r.done:
		if err != nil {
			r.t.Errorf("Run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		r.t.Fatal("Run did not return once its context was done")
	}
	r.done <- nil // for the cleanup
}

// TestCancelStopsAScan ends Run while a scan waits, as one does on the lock
// of a command serve started: Run returns at once rather than once the scan
// does, which would be once that command ends, and the command ends only
// once serve has. The refresh that waits for the scan ends with its door's
// request, which serve ending cancels.
func TestCancelStopsAScan(t *testing.T) {
	t.Parallel()
	scans := 0
	waiting := make(chan struct{})
	r := start(t, Options{
		Scan: func(ctx context.Context) (scan.Inventory, error) {
			if scans++; scans == 1 {
				return scan.Inventory{}, nil // the initial scan
			}
			close(waiting)
			<-ctx.Done() // a lock that is never let go
			return scan.Inventory{}, ctx.Err()
		},
	})
	answered := make(chan error, 1)
	go func() { _, err := r.refresh(); answered <- err }()
	<-waiting
	r.stop()
	if err := <-answered; err == nil {
		t.Error("the refresh was answered as if its scan had ended")
	}
}

// TestServeGoesOnFromAFailedInitialScan fails the initial scan: the failure
// is reported, no job starts, and the refresh that scans again with success
// emits the first inventory, counter 1, after which the jobs start.
func TestServeGoesOnFromAFailedInitialScan(t *testing.T) {
	t.Parallel()
	broken := errors.New("git is missing")
	scans := 0
	failed := make(chan error, 1)
	inventories := make(chan int, 2)
	checked := make(chan int, 1)
	r := start(t, Options{
		Scan: func(context.Context) (scan.Inventory, error) {
			if scans++; scans == 1 {
				return scan.Inventory{}, broken
			}
			return scan.Inventory{}, nil
		},
		Inventory: func(inv scan.Inventory) { inventories <- inv.ScanCounter },
		Fail:      func(err error) { failed <- err },
		Check: func(context.Context) func() {
			checked <- len(inventories)
			return nil
		},
	})
	if err := <-failed; !errors.Is(err, broken) {
		t.Fatalf("Fail got %v, want the scan's error", err)
	}
	counter, err := r.refresh()
	if err != nil || counter != 1 {
		t.Fatalf("the refresh answered %d, %v, want inventory 1", counter, err)
	}
	if got := <-inventories; got != 1 {
		t.Errorf("the first inventory has counter %d, want 1", got)
	}
	select {
	case <-checked:
	case <-time.After(10 * time.Second):
		t.Fatal("the check did not start once the first inventory was out")
	}
	if scans != 2 {
		t.Errorf("%d scans, want the initial one and the refresh's", scans)
	}
}

// TestServeGoesOnWithoutWatching watches a directory that is a symlink
// loop, which cannot be watched: the failure is reported as ErrWatch, and
// a refresh still scans.
func TestServeGoesOnWithoutWatching(t *testing.T) {
	t.Parallel()
	loop := filepath.Join(t.TempDir(), "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	failed := make(chan error, 1)
	r := start(t, Options{
		Scan:  func(context.Context) (scan.Inventory, error) { return scan.Inventory{}, nil },
		Watch: []string{loop},
		Fail:  func(err error) { failed <- err },
	})
	if err := <-failed; !errors.Is(err, ErrWatch) {
		t.Fatalf("Fail got %v, want ErrWatch", err)
	}
	if counter, err := r.refresh(); err != nil || counter != 1 {
		t.Fatalf("the refresh answered %d, %v, want inventory 1", counter, err)
	}
}

// TestRefuseAnswersEveryRefresh refuses the requests of a serve that runs
// no loop: a refresh waiting is answered with the refusal, and one after it
// with what trying again gave.
func TestRefuseAnswersEveryRefresh(t *testing.T) {
	t.Parallel()
	r := NewRequests()
	refused := errors.New("another serve is running")
	waiting := make(chan error, 1)
	go func() { _, err := r.Refresh(context.Background()); waiting <- err }()
	tries := 0
	r.Refuse(refused, func() error { tries++; return refused })
	if err := <-waiting; !errors.Is(err, refused) {
		t.Errorf("the refresh waiting got %v", err)
	}
	before := tries
	if _, err := r.Refresh(context.Background()); !errors.Is(err, refused) {
		t.Errorf("a later refresh got %v", err)
	}
	if tries != before+1 {
		t.Errorf("a later refresh tried again %d times, want 1", tries-before)
	}
	if got, err := r.Search(context.Background(), "x"); err != nil || len(got) != 0 {
		t.Errorf("a search found %v, %v", got, err)
	}
}

// TestRefusalEndsWhenTryingAgainWorks ends a refusal: the refresh whose
// try worked, and every one after it, is answered by the Run that starts
// then, and tries nothing again.
func TestRefusalEndsWhenTryingAgainWorks(t *testing.T) {
	t.Parallel()
	r := NewRequests()
	refused := errors.New("another serve is running")
	free := false
	tries := 0
	r.Refuse(refused, func() error {
		tries++
		if !free {
			return refused
		}
		return nil
	})
	if _, err := r.Refresh(context.Background()); !errors.Is(err, refused) {
		t.Fatalf("a refresh while refused got %v", err)
	}
	free = true
	refreshed := make(chan error, 1)
	go func() { _, err := r.Refresh(context.Background()); refreshed <- err }()

	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan error, 1)
	go func() {
		ran <- Run(ctx, Options{
			Scan:      func(context.Context) (scan.Inventory, error) { return scan.Inventory{}, nil },
			Requests:  r,
			Inventory: func(scan.Inventory) {},
			Fail:      func(err error) { t.Errorf("Fail(%v)", err) },
			Warn:      func(string) {},
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-ran
	})
	if err := <-refreshed; err != nil {
		t.Fatalf("the refresh that found it free got %v", err)
	}
	if _, err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("a refresh after it got %v", err)
	}
	if tries != 2 {
		t.Errorf("tried again %d times, want 2", tries)
	}
	if got, err := r.Search(context.Background(), "x"); err != nil || len(got) != 0 {
		t.Errorf("a search found %v, %v", got, err)
	}
}
