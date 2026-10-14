package interchange

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestBoundDefectCandidatesRequiresArchivedBytesAndSourcePair(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/defect-proposal-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var projection struct {
		Image v2input.Ref `json:"image"`
	}
	if err := json.Unmarshal(raw, &projection); err != nil {
		t.Fatal(err)
	}
	parsed := importParsedValidatorFixture()
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}
	staged := v2input.Ref{ArtifactID: uuid.New(), SHA256: parent.SHA256}
	parsed.Manifest.Inventory = append(parsed.Manifest.Inventory, importBundleInventory{ID: parent.ArtifactID, Path: "documents/defect_candidates.json", SHA: parent.SHA256, Contract: "DefectCandidates"})
	parsed.Files = append(parsed.Files, importParsedFile{Path: "documents/defect_candidates.json", Content: staged, Contract: "DefectCandidates"})
	parsedRaw, err := canonical.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: projection.Image, CanvasHash: rawSHA([]byte("canvas"))}
	target := old
	target.AssetVersionID = uuid.New()
	target.Source.ArtifactID = uuid.New()
	ports := &importRecipePorts{old: old, target: target}
	semantic := &importDefectCandidatesQualifierStub{}
	q := BoundImportDefectCandidatesQualifier{Entry: parsed.Manifest.Entries[0].ID, Old: old, Target: target, Parsed: parsedRaw, Reader: remapArchiveStub{staged.ArtifactID: raw}, Sources: ports, Semantic: semantic}
	if err := q.ValidateArchivedImportDefectCandidates(context.Background(), raw, old); err != nil || semantic.archived != 1 {
		t.Fatal("qualified archive refused", err)
	}
	wrong := append([]byte(nil), raw...)
	wrong[len(wrong)-1] = ' '
	if err := q.ValidateArchivedImportDefectCandidates(context.Background(), wrong, old); err == nil {
		t.Fatal("changed proposal accepted")
	}
	changed := target
	changed.AssetVersionID = uuid.New()
	if err := q.QualifyImportedDefectCandidates(context.Background(), raw, changed); err == nil {
		t.Fatal("foreign target accepted")
	}
}
