package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"strings"
	"testing"
)

func TestShownRemovalSQLResolverRequiresScopedDependencies(t *testing.T) {
	if _, err := NewSQLShownRemovalApplyResolver(nil, nil, nil, 1048576, "a", "b", nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestShownRemovalResolverRejectsCancellationAndMissingIdentityBeforeIO(t *testing.T) {
	db := &pgxpool.Pool{}
	read := func(context.Context, PlanSource) ([]byte, error) { t.Fatal("unexpected object read"); return nil, nil }
	budgets := func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, ApplyDecision) ([]ExposureInputBudget, error) {
		t.Fatal("unexpected budget selection")
		return nil, nil
	}
	resolve, err := NewSQLShownRemovalApplyResolver(db, &RemovalPreviewProvider{}, read, 1048576, strings.Repeat("a", 64), strings.Repeat("b", 64), budgets)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = resolve(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, ApplyDecision{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = resolve(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), ApplyDecision{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestShownRemovalSourceLoaderRejectsMissingDependenciesAndCancellationBeforeIO(t *testing.T) {
	read := func(context.Context, PlanSource) ([]byte, error) { t.Fatal("unexpected read"); return nil, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := LoadShownRemovalSourceDocuments(ctx, nil, auth.Identity{}, uuid.Nil, uuid.Nil, ScopedPreviewReview{}, PreviewRequest{}, read, ExposureOwnerCosts{}, ExposureResourceEstimate{}, ExposureQualificationPin{}, RemovalPreviewParams{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err := LoadShownRemovalSourceDocuments(context.Background(), nil, auth.Identity{}, uuid.Nil, uuid.Nil, ScopedPreviewReview{}, PreviewRequest{}, read, ExposureOwnerCosts{}, ExposureResourceEstimate{}, ExposureQualificationPin{}, RemovalPreviewParams{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
