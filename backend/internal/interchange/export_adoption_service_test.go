package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestExportResultAdoptionServiceFailsBeforeCandidateIOWithoutInstallation(t *testing.T) {
	service := ExportResultAdoptionService{}
	if _, err := service.AdoptCurrentExportResult(context.Background(), auth.Identity{},
		uuid.Nil, CreateRequest{}, "key"); err != v2input.ErrInvalid {
		t.Fatal("missing process installation accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.AdoptCurrentExportResult(ctx, auth.Identity{},
		uuid.Nil, CreateRequest{}, "key"); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
