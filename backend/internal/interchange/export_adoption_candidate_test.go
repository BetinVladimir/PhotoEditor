package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestRecoverExportAdoptionCandidateRejectsMissingDatabaseBeforeIO(t *testing.T) {
	if _, err := RecoverExportAdoptionCandidate(context.Background(), nil,
		auth.Identity{}, uuid.Nil, CreateRequest{}, "key"); err != v2input.ErrInvalid {
		t.Fatal("missing database accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RecoverExportAdoptionCandidate(ctx, nil,
		auth.Identity{}, uuid.Nil, CreateRequest{}, "key"); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
