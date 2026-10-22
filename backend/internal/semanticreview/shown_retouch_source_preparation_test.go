package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestShownRetouchSourceCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, transform, err := LoadShownRetouchSourceDocuments(ctx, nil, auth.Identity{}, uuid.New(), uuid.New(), ScopedPreviewReview{}, PreviewRequest{}, nil, ExposureOwnerCosts{}, ExposureResourceEstimate{}, RetouchQualificationPin{}, RetouchPreviewParams{})
	if err != context.Canceled || transform != nil || got.TargetID != uuid.Nil {
		t.Fatal(got, err)
	}
}
func TestShownRetouchSQLResolverRequiresOwnPhysicalProvider(t *testing.T) {
	got, err := NewSQLShownRetouchApplyResolver(nil, nil, nil, 1, "", "", nil)
	if err != ErrInvalid || got != nil {
		t.Fatal("missing scoped dependencies accepted", err)
	}
}
