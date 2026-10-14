package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestImportMaskCandidateTargetRejectsMissingAndCancelledAuthority(t *testing.T) {
	source := SQLImportMaskCandidateTarget{}
	if _, err := source.Resolve(context.Background(), []byte(`{}`), uuid.New()); err == nil {
		t.Fatal("accepted target without tenant authority")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Resolve(ctx, []byte(`{}`), uuid.New()); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
}
