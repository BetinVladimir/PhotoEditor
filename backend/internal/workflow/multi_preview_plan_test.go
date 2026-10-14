package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"testing"
)

func TestMultiPreviewPlanRefusesUnverifiedContentAndZeroPlan(t *testing.T) {
	if _, err := CompileMultiPreviewPlan(v2render.MultiPreviewSource{}, v2render.MultiResultContent{}, "invalid"); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	if err := validateMultiPreviewPlan(MultiPreviewPlan{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := (MultiPreviewIntentStore{}).Save(context.Background(), MultiPreviewPlan{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	id, err := (MultiPreviewIntentStore{}).Save(ctx, MultiPreviewPlan{})
	if !errors.Is(err, context.Canceled) || id != uuid.Nil {
		t.Fatal(err)
	}
}
