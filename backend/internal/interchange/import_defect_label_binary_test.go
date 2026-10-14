package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestImportDefectLabelBinaryRequiresOccurrencePublisher(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("labels"))}
	r := ImportDefectLabelBinaryRemapper{}
	_, _, err := r.RemapImportBinary(context.Background(), uuid.New(), ref, ref, "DefectLabelMap", v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{}, 1<<20)
	var unsupported *ImportRecipeUnsupported
	if !errors.As(err, &unsupported) || unsupported.Reason != "defect_label_occurrence_publisher_not_installed" {
		t.Fatalf("unwitnessed labels accepted: %v", err)
	}
	_, _, err = r.RemapImportBinary(context.Background(), uuid.New(), ref, ref, "MaskPixels", v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{}, 1<<20)
	if !errors.As(err, &unsupported) || unsupported.Reason != "binary_dependency_codec_not_installed" {
		t.Fatalf("foreign binary role accepted: %v", err)
	}
}
