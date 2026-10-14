package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestRestorationPreviewSealRefusesMissingPhysicalWitness(t *testing.T) {
	plan, _, before, after := restorationPreviewFixture(t)
	if err := SealRestorationPreview(context.Background(), nil, plan,
		before, after, "photos", VerifiedRestorationPreviewInputs{}); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal("uninstalled seal accepted", err)
	}
	bad := plan
	bad.docs[2].ID = uuid.New()
	if err := SealRestorationPreview(context.Background(), nil, bad,
		before, after, "photos", VerifiedRestorationPreviewInputs{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("substituted task accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SealRestorationPreview(ctx, nil, plan, before, after,
		"photos", VerifiedRestorationPreviewInputs{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled seal accepted", err)
	}
}
