package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestShownRetouchBatchPreparationCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PrepareShownRetouchApplyBatch(ctx, nil, nil, "owned", auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil,
		PreparedReview{}, nil, ApplyDecision{}, nil, nil)
	if err != context.Canceled {
		t.Fatal(err)
	}
}

func TestShownRetouchBatchPreparationRejectsMissingPublicationDependencies(t *testing.T) {
	_, err := PrepareShownRetouchApplyBatch(context.Background(), nil, nil, "owned", auth.Identity{}, uuid.New(), uuid.New(), uuid.New(),
		PreparedReview{}, nil, ApplyDecision{}, nil, func(context.Context) error { t.Fatal("unexpected authority callback"); return nil })
	if err == nil {
		t.Fatal("missing dependencies accepted")
	}
}

func TestRetouchBatchCoreRejectsInvalidDecisionBeforePublication(t *testing.T) {
	calls := 0
	publication := retouchBatchPublication{
		approved: func(context.Context, ApplyDecision) error { calls++; return nil },
		copyPublisher: func(ShownRetouchPreparationInput, FrozenShownRetouchTask) (v2input.Publisher, error) {
			calls++
			return nil, ErrInvalid
		},
		decision: func(context.Context, ApplyDecision) (PublishedDecision, error) {
			calls++
			return PublishedDecision{}, ErrInvalid
		},
	}
	result, err := prepareShownRetouchBatch(context.Background(), uuid.New(), uuid.New(), uuid.New(), PreparedReview{}, nil,
		ApplyDecision{}, nil, func(context.Context) error { calls++; return nil }, publication)
	if err == nil || calls != 0 || result.Child != uuid.Nil || len(result.ShownTasks) != 0 {
		t.Fatal("invalid batch published", result, err, calls)
	}
}

func TestRetouchBatchCoreCancellationBeforePublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := prepareShownRetouchBatch(ctx, uuid.Nil, uuid.Nil, uuid.Nil, PreparedReview{}, nil, ApplyDecision{}, nil, nil, retouchBatchPublication{})
	if err != context.Canceled || result.Child != uuid.Nil || len(result.ShownTasks) != 0 {
		t.Fatal("canceled batch escaped", result, err)
	}
}
