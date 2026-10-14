package v2learning

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestPortraitDetailPreparationRefusesCancelledAndUnboundMaterial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PreparePortraitDetailHoldout(ctx, TrainingStart{},
		TrainingCandidate{}, TrainingRenderCase{},
		PortraitDetailProducerCandidate{}, nil, nil, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled preparation did not stop before material")
	}
	if _, err := PreparePortraitDetailHoldout(context.Background(),
		TrainingStart{TenantID: uuid.New()},
		TrainingCandidate{EventID: uuid.New()},
		TrainingRenderCase{Protected: v2input.Ref{ArtifactID: uuid.New(),
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		PortraitDetailProducerCandidate{}, nil, nil, nil, nil); !errors.Is(err, ErrInvalidTrainingDraft) {
		t.Fatal("unbound producer admitted")
	}
}
