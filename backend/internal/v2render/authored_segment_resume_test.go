package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredSegmentAdmissionCancellationBeforeStorage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := SQLAuthoredSegmentAdmission{}
	if err := s.Seal(ctx, uuid.New(), uuid.New(), 1, AuthoredSegmentTaskPlan{}, v2input.GraphTask{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("seal cancellation identity: %v", err)
	}
	if err := s.Admit(ctx, uuid.New(), uuid.New(), 1, AuthoredSegmentTaskPlan{}, "", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("admit cancellation identity: %v", err)
	}
	state, err := (SQLAuthoredSegmentPreparation{}).ResumeAdmission(ctx, auth.Identity{}, uuid.New(), uuid.New(), 1, nil, nil, nil)
	if state != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("resume cancellation identity: %q %v", state, err)
	}
}

func TestAuthoredSegmentAdmissionMissingConcreteDependenciesRefused(t *testing.T) {
	ctx := context.Background()
	tenant, parent := uuid.New(), uuid.New()
	if err := (SQLAuthoredSegmentAdmission{}).Admit(ctx, tenant, parent, 1, AuthoredSegmentTaskPlan{}, "bucket", nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("nil object storage: %v", err)
	}
	state, err := (SQLAuthoredSegmentPreparation{}).ResumeAdmission(ctx, auth.Identity{}, tenant, parent, 1, nil, nil, nil)
	if state != "" || !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("missing authority: %q %v", state, err)
	}
	if errors.Is(ErrAuthoredSegmentPending, v2input.ErrInvalid) || errors.Is(ErrAuthoredSegmentPending, context.Canceled) {
		t.Fatal("pending must retain distinct retry identity")
	}
}
