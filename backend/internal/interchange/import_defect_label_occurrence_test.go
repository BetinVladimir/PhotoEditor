package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestDefectLabelOccurrenceBindsParentGeometryAndIdentity(t *testing.T) {
	workflow, entry := uuid.New(), uuid.New()
	original := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("labels"))}
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("proposal"))}
	mapped := v2input.Ref{ArtifactID: uuid.NewSHA1(workflow, []byte("interchange:dependency:"+entry.String()+":DefectLabelMap:"+original.ArtifactID.String())), SHA256: original.SHA256}
	scope := ImportPreparedScope{Workflow: workflow, Entry: entry, Contract: "DefectLabelMap", DefectLabel: &ImportDefectLabelOccurrence{Original: original, Parent: parent, Width: 400, Height: 300}}
	if err := validateImportDefectLabelOccurrence(scope, mapped); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"contract", "parent", "width", "area", "sha", "id", "lut"} {
		candidate := scope
		owned := *scope.DefectLabel
		candidate.DefectLabel = &owned
		ref := mapped
		switch mode {
		case "contract":
			candidate.Contract = "MaskPixels"
		case "parent":
			owned.Parent = original
		case "width":
			owned.Width = 0
		case "area":
			owned.Width = 200000000
			owned.Height = 2
		case "sha":
			ref.SHA256 = rawSHA([]byte("wrong"))
		case "id":
			ref.ArtifactID = uuid.New()
		case "lut":
			candidate.LUT = &ImportLUTOccurrence{}
		}
		if err := validateImportDefectLabelOccurrence(candidate, ref); err == nil {
			t.Fatal("invalid label occurrence accepted", mode)
		}
	}
	if _, err := (PreparedImportArtifactPublisher{}).PublishImportDefectLabel(context.Background(), entry, mapped, objectstore.Object{}, original, parent, 400, 300); err == nil {
		t.Fatal("unscoped publisher accepted labels")
	}
}
