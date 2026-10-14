package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestBeginDraftRejectsUnavailablePoolAndCancellation(t *testing.T) {
	result, err := BeginDraft(context.Background(), nil, auth.Identity{}, Request{}, "", PublishedIntent{}, WorkflowPlan{}, 0)
	if !errors.Is(err, v2input.ErrInvalid) || result != (DraftAdmission{}) {
		t.Fatal("missing scheduler pool", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = BeginDraft(ctx, nil, auth.Identity{}, Request{}, "", PublishedIntent{}, WorkflowPlan{}, 0)
	if !errors.Is(err, context.Canceled) || result != (DraftAdmission{}) {
		t.Fatal("cancelled draft", result, err)
	}
}
