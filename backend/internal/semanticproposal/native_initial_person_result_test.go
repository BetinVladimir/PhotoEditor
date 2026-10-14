package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestOriginalNativePersonInputRejectsBeforeIO(t *testing.T) {
	reader := SQLTransferSourceReader{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.readOriginalNativePersonInput(ctx, auth.Identity{}, personDraftPlan{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := reader.readOriginalNativePersonInput(context.Background(), auth.Identity{}, personDraftPlan{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	plan := personDraftPlan{}
	var err error
	plan.seal, err = personPlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reader.readOriginalNativePersonInput(context.Background(), auth.Identity{}, plan); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("missing database accepted", err)
	}
	plan.key = "substituted"
	if _, err = reader.readOriginalNativePersonInput(context.Background(), auth.Identity{}, plan); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("tampered plan accepted", err)
	}
}
