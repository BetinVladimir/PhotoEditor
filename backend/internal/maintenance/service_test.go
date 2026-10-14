package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"
)

type jobFunc func(context.Context) (bool, error)

func (f jobFunc) CleanOne(ctx context.Context) (bool, error) { return f(ctx) }
type shortBudgetJob func(context.Context) (bool, error)

func (f shortBudgetJob) CleanOne(ctx context.Context) (bool, error) { return f(ctx) }
func (shortBudgetJob) CleanupCycleTimeout() time.Duration          { return time.Millisecond }

func TestLoopRejectsLateSuccessAfterCycleDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := new(LoopState)
	entered := make(chan int, 2)
	calls := 0
	job := shortBudgetJob(func(cycle context.Context) (bool, error) {
		calls++
		entered <- calls
		<-cycle.Done()
		return true, nil // Simulate an object store that ignores cancellation.
	})
	done := make(chan error, 1)
	go func() { done <- RunLoop(ctx, job, state, time.Millisecond) }()
	for want := 1; want <= 2; want++ {
		select {
		case got := <-entered:
			if got != want {
				t.Fatalf("cycle = %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("cleanup loop stalled")
		}
	}
	if state.Ready.Load() || state.Cleaned.Load() != 0 || state.Failures.Load() != 1 {
		t.Fatal("expired cleanup cycle counted as success")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup loop ignored shutdown")
	}
}

func TestLoopRecordsFailureAndStopsActiveJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := new(LoopState)
	entered := make(chan int, 3)
	release := make(chan struct{})
	calls := 0
	job := jobFunc(func(ctx context.Context) (bool, error) {
		calls++
		entered <- calls
		switch calls {
		case 1:
			return true, nil
		case 2:
			select {
			case <-release:
				return false, errors.New("dependency unavailable")
			case <-ctx.Done():
				return false, ctx.Err()
			}
		default:
			<-ctx.Done()
			return false, ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() { done <- RunLoop(ctx, job, state, time.Millisecond) }()
	wait := func(expected int) {
		t.Helper()
		select {
		case got := <-entered:
			if got != expected {
				t.Fatal(got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("loop did not advance")
		}
	}
	wait(1)
	wait(2)
	if state.Cleaned.Load() != 1 || !state.Ready.Load() {
		t.Fatal("successful cleanup not counted")
	}
	close(release)
	wait(3)
	if state.Ready.Load() || state.Failures.Load() != 1 {
		t.Fatal("failure not reflected in readiness")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight cleanup ignored cancellation")
	}
	if state.Ready.Load() || state.Failures.Load() != 1 {
		t.Fatal("shutdown treated as dependency failure")
	}
}

func TestJobsRotateAndPreserveEachReadiness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	states := []*LoopState{new(LoopState), new(LoopState)}
	entered := make(chan int, 1)
	release := make(chan struct{})
	calls := [2]int{}
	makeJob := func(index int) CleanupJob {
		return jobFunc(func(ctx context.Context) (bool, error) {
			entered <- index
			calls[index]++
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-release:
			}
			if index == 1 && calls[index] == 1 {
				return false, errors.New("document storage failure")
			}
			return false, nil
		})
	}
	done := make(chan error, 1)
	go func() { done <- RunJobs(ctx, []CleanupJob{makeJob(0), makeJob(1)}, states, time.Millisecond) }()
	wait := func(want int) {
		t.Helper()
		select {
		case got := <-entered:
			if got != want {
				t.Fatal("unexpected order", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("job starved")
		}
	}
	wait(0)
	release <- struct{}{}
	wait(1)
	if !states[0].Ready.Load() || states[1].Ready.Load() {
		t.Fatal("unchecked job became ready")
	}
	release <- struct{}{}
	wait(0)
	if states[1].Ready.Load() || states[1].Failures.Load() != 1 {
		t.Fatal("document failure missing")
	}
	release <- struct{}{}
	wait(1)
	if !states[0].Ready.Load() || states[1].Ready.Load() {
		t.Fatal("other success erased failed readiness")
	}
	release <- struct{}{}
	wait(0)
	if !states[0].Ready.Load() || !states[1].Ready.Load() || states[1].Failures.Load() != 1 {
		t.Fatal("recovery/counters invalid")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel active job")
	}
	for _, state := range states {
		if state.Ready.Load() {
			t.Fatal("ready after shutdown")
		}
	}
}

func TestJobsRejectSharedState(t *testing.T) {
	state := new(LoopState)
	job := jobFunc(func(context.Context) (bool, error) { t.Fatal("invalid loop ran"); return false, nil })
	if err := RunJobs(context.Background(), []CleanupJob{job, job}, []*LoopState{state, state}, time.Second); err == nil {
		t.Fatal("shared state accepted")
	}
}

func TestTenantCleanerHasBoundedTwoScanCycle(t *testing.T) {
	if got := cleanupCycleTimeout(TenantCleaner{}); got != 120*time.Second {
		t.Fatal("tenant deletion cycle cannot fit two physical scans", got)
	}
	if got := cleanupCycleTimeout(jobFunc(func(context.Context) (bool, error) { return false, nil })); got != 50*time.Second {
		t.Fatal("ordinary cleanup budget changed", got)
	}
}
