package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

// These tests exercise only driver orchestration. A stubbed compute outcome is
// never evidence that a real operation, corpus or qualification passed.
type measurementStoreStub struct {
	ApplyPhaseStore
	done     bool
	tasks    []PreparedTask
	accepted int
}

func (s *measurementStoreStub) Claim(context.Context) (*ApplyLease, error)        { return nil, nil }
func (s *measurementStoreStub) MeasurementComplete(context.Context) (bool, error) { return s.done, nil }
func (s *measurementStoreStub) PendingMeasuredTasks(context.Context) ([]PreparedTask, error) {
	return s.tasks, nil
}
func (s *measurementStoreStub) AcceptMeasuredTask(_ context.Context, _ PreparedTask, _ OperatorMeasuredTask) error {
	s.accepted++
	s.done = true
	return nil
}

type measurementComputeStub struct {
	fail   bool
	mutate bool
}

func (c measurementComputeStub) Execute(_ context.Context, t PreparedTask) (OperatorMeasuredTask, error) {
	if c.fail {
		return OperatorMeasuredTask{}, errors.New("physical compute refused")
	}
	if c.mutate {
		t.InputDigest = "wrong"
	}
	return OperatorMeasuredTask{Task: t, OutputBytes: 12, ObservationSHA256: strings.Repeat("a", 64)}, nil
}
func TestOperatorMeasurementRequiresDeadline(t *testing.T) {
	s := &measurementStoreStub{}
	if _, e := MeasureOperatorApply(context.Background(), ApplyCoordinator{}, s, measurementComputeStub{}, 2); e == nil {
		t.Fatal("unbounded measurement accepted")
	}
}
func TestOperatorMeasurementExplicitMissingDecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s := &measurementStoreStub{}
	if _, e := MeasureOperatorApply(ctx, ApplyCoordinator{}, s, measurementComputeStub{}, 2); e == nil || s.accepted != 0 {
		t.Fatal("no-progress barrier accepted")
	}
}
func TestOperatorMeasurementBoundObservation(t *testing.T) {
	for _, kind := range []string{"valid", "compute_failed", "wrong_input_digest"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			s := &measurementStoreStub{tasks: []PreparedTask{{ID: uuid.New(), Operation: "geometry.crop", Version: "1", TimeoutSeconds: 1, InputDigest: "actual"}}}
			result, e := MeasureOperatorApply(ctx, ApplyCoordinator{}, s, measurementComputeStub{fail: kind == "compute_failed", mutate: kind == "wrong_input_digest"}, 3)
			if kind == "valid" {
				if e != nil || len(result) != 1 || s.accepted != 1 || result[0].Elapsed <= 0 {
					t.Fatal("valid observation rejected", e)
				}
			} else if e == nil || s.accepted != 0 {
				t.Fatal("failed observation accepted")
			}
		})
	}
}

type ignoreExpiredCompute struct{}

func (ignoreExpiredCompute) Execute(ctx context.Context, t PreparedTask) (OperatorMeasuredTask, error) {
	<-ctx.Done()
	return OperatorMeasuredTask{Task: t, OutputBytes: 12, ObservationSHA256: strings.Repeat("a", 64)}, nil
}
func TestOperatorMeasurementRejectsIgnoredDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := &measurementStoreStub{tasks: []PreparedTask{{ID: uuid.New(), Operation: "geometry.crop", Version: "1", TimeoutSeconds: 1, InputDigest: "actual"}}}
	_, e := MeasureOperatorApply(ctx, ApplyCoordinator{}, s, ignoreExpiredCompute{}, 3)
	if !errors.Is(e, context.DeadlineExceeded) || ctx.Err() != nil || s.accepted != 0 {
		t.Fatal("expired compute accepted", e)
	}
}

type upperCaseObservationCompute struct{}

func (upperCaseObservationCompute) Execute(_ context.Context, t PreparedTask) (OperatorMeasuredTask, error) {
	return OperatorMeasuredTask{Task: t, OutputBytes: 12, ObservationSHA256: strings.Repeat("A", 64)}, nil
}
func TestOperatorMeasurementRejectsUppercaseDigest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s := &measurementStoreStub{tasks: []PreparedTask{{ID: uuid.New(), Operation: "geometry.crop", Version: "1", TimeoutSeconds: 1, InputDigest: "actual"}}}
	if _, e := MeasureOperatorApply(ctx, ApplyCoordinator{}, s, upperCaseObservationCompute{}, 3); e == nil || s.accepted != 0 {
		t.Fatal("noncanonical digest accepted")
	}
}
