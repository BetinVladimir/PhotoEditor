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

func TestArchivedDefectLabelSourceRequiresQualifiedParent(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/defect-proposal-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var refs struct {
		Image  v2input.Ref `json:"image"`
		Labels v2input.Ref `json:"label_map"`
	}
	if err := json.Unmarshal(raw, &refs); err != nil {
		t.Fatal(err)
	}
	parsed := importParsedValidatorFixture()
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}
	staged := v2input.Ref{ArtifactID: uuid.New(), SHA256: parent.SHA256}
	parsed.Manifest.Inventory = append(parsed.Manifest.Inventory,
		importBundleInventory{ID: parent.ArtifactID, Path: "documents/defect_candidates.json", SHA: parent.SHA256, Contract: "DefectCandidates"},
		importBundleInventory{ID: refs.Labels.ArtifactID, Path: "arrays/defect_labels.npy", SHA: refs.Labels.SHA256, Contract: "DefectLabelMap"})
	parsed.Files = append(parsed.Files,
		importParsedFile{Path: "documents/defect_candidates.json", Content: staged, Contract: "DefectCandidates"},
		importParsedFile{Path: "arrays/defect_labels.npy", Content: v2input.Ref{ArtifactID: uuid.New(), SHA256: refs.Labels.SHA256}, Contract: "DefectLabelMap"})
	parsedRaw, err := canonical.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	entry := v2input.Entry{Ref: refs.Labels, Contract: "DefectLabelMap"}
	qualifier := &importDefectCandidatesQualifierStub{}
	source := ImportArchivedDefectLabelSource{Parsed: parsedRaw, Reader: remapArchiveStub{staged.ArtifactID: raw}, Qualifier: qualifier, Old: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: refs.Image}}
	result, err := source.ResolveImportDefectLabelParent(context.Background(), entry)
	if err != nil || result != parent || qualifier.archived != 1 {
		t.Fatal("label parent was not qualified", result, err)
	}
	source.Qualifier = nil
	if _, err := source.ResolveExportDefectLabels(context.Background(), entry); err == nil {
		t.Fatal("unqualified proposal accepted")
	}
}
