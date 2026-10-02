package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

type activationSource []BoundTaskActivation

func (s activationSource) Pending(context.Context, int) ([]BoundTaskActivation, error) { return s, nil }

type activationTarget struct {
	calls  int
	failAt int
}

func (a *activationTarget) Activate(context.Context, BoundTaskActivation) (bool, error) {
	a.calls++
	if a.calls == a.failAt {
		return false, errors.New("database lost")
	}
	return a.calls != 1, nil
}
func TestIngestActivationContinuesAfterStaleAndStopsOnError(t *testing.T) {
	source := activationSource{{TaskID: uuid.New()}, {TaskID: uuid.New()}, {TaskID: uuid.New()}}
	target := &activationTarget{failAt: 3}
	runner := IngestActivator{Source: source, Tasks: target}
	count, err := runner.Advance(context.Background(), 3)
	if err == nil || count != 1 || target.calls != 3 {
		t.Fatal("wrong partial result", count, err)
	}
	target.calls = 0
	target.failAt = 0
	count, err = runner.Advance(context.Background(), 3)
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	target.calls = 0
	if _, err = runner.Advance(context.Background(), 2); err == nil || target.calls != 0 {
		t.Fatal("overlimit batch executed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = runner.Advance(ctx, 3); err != context.Canceled || target.calls != 0 {
		t.Fatal("cancelled batch executed")
	}
}
