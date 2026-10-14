package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestExportResultLearningOccurrenceRequiresCanonicalCatalog(t *testing.T) {
	if err := (SQLExportResultInputCatalog{}).QualifyExportLearningOccurrence(
		context.Background(), v2draft.LayerRef{}, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("learning occurrence accepted without canonical input catalog", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (SQLExportResultInputCatalog{}).QualifyExportLearningOccurrence(
		ctx, v2draft.LayerRef{}, []byte("{}")); err != context.Canceled {
		t.Fatal("learning occurrence lost cancellation", err)
	}
}
