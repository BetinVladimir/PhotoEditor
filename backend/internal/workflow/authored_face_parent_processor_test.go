package workflow

import (
	"context"
	"errors"
	"testing"
)

func TestAuthoredFaceParentCancellationBeforeDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (*AuthoredFaceParentProcessor)(nil).ProcessOne(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation identity lost: %v", err)
	}
	if _, err := (*AuthoredFaceParentPreparation)(nil).Resume(ctx, [16]byte{}, [16]byte{}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("resume cancellation identity lost: %v", err)
	}
}
func TestAuthoredFaceSchedulerUnconfiguredIsUnavailable(t *testing.T) {
	for _, name := range []string{"PHOTO_AUTHORED_FACE_DEPLOYMENT", "PHOTO_AUTHORED_FACE_DEPLOYMENT_SHA256", "PHOTO_AUTHORED_FACE_API_DATABASE_URL"} {
		t.Setenv(name, "")
	}
	p, closeFn, err := buildAuthoredFaceParentScheduler(context.Background(), nil, nil, "", "")
	if err != nil || p != nil || closeFn == nil {
		t.Fatalf("unconfigured scheduler synthesized runtime: %v", err)
	}
	closeFn()
	if _, err = (*AuthoredFaceParentProcessor)(nil).ProcessOne(context.Background()); !errors.Is(err, ErrAuthoredFaceUnavailable) {
		t.Fatalf("missing runtime accepted: %v", err)
	}
	t.Setenv("PHOTO_AUTHORED_FACE_DEPLOYMENT", "not-present")
	if p, _, err = buildAuthoredFaceParentScheduler(context.Background(), nil, nil, "", ""); err == nil || p != nil {
		t.Fatal("partial deployment configured without pins")
	}
}
