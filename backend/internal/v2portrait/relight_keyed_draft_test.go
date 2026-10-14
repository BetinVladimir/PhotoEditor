package v2portrait

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRelightDraftRefusesMissingIdentityBeforeDatabase(t *testing.T) {
	s := &RelightSubmissionService{}
	for _, who := range []auth.Identity{{}, {UserID: uuid.New()}, {SessionID: uuid.New()}} {
		if err := s.CreateDraft(context.Background(), who, RelightSubmissionRequest{}); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := s.CreateKeyedDraft(context.Background(), who, "valid-key", RelightDraftIntent{}); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.CreateDraft(ctx, auth.Identity{}, RelightSubmissionRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.CreateKeyedDraft(ctx, auth.Identity{}, "valid-key", RelightDraftIntent{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
