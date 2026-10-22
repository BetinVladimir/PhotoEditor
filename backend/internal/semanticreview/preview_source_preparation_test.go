package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPreviewSourcePreparationRequiresScopedDatabaseBeforeIO(t *testing.T) {
	read := func(context.Context, PlanSource) ([]byte, error) { t.Fatal("unscoped source IO"); return nil, nil }
	got, transform, err := LoadPreviewSourceDocuments(context.Background(), nil, auth.Identity{}, uuid.New(), uuid.New(), ScopedPreviewReview{}, PreviewRequest{}, read,
		ExposureOwnerCosts{}, ExposureResourceEstimate{}, ExposureQualificationPin{})
	if err == nil || got.TargetID != uuid.Nil || transform != nil {
		t.Fatal("partial unscoped source returned")
	}
}
