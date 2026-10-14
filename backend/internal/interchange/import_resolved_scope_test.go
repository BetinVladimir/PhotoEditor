package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestBoundImportResolvedScopeRequiresExactArchivedInventoryAndSourcePair(t *testing.T) {
	parsed := importParsedValidatorFixture()
	oldImage := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("old-image"))}
	newImage := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("new-image"))}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("style"))}
	for _, item := range []struct {
		ref        v2input.Ref
		kind, path string
	}{{oldImage, "WorkingImage", "documents/old-image.json"}, {style, "StyleV2", "documents/style.json"}} {
		parsed.Manifest.Inventory = append(parsed.Manifest.Inventory, importBundleInventory{ID: item.ref.ArtifactID, SHA: item.ref.SHA256, Contract: item.kind, Path: item.path})
		parsed.Files = append(parsed.Files, importParsedFile{Path: item.path, Content: v2input.Ref{ArtifactID: uuid.New(), SHA256: item.ref.SHA256}, Contract: item.kind})
	}
	raw, err := canonical.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: oldImage, CanvasHash: rawSHA([]byte("canvas"))}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: newImage, CanvasHash: old.CanvasHash}
	source := &importRecipePorts{old: old, target: target}
	scope, err := NewBoundImportResolvedScope(raw, []ImportCorrectionBinding{{Entry: parsed.Manifest.Entries[0].ID, Old: old, New: target}}, source, &importMasterReaderStub{}, rawSHA([]byte("icc")))
	if err != nil {
		t.Fatal(err)
	}
	children := []v2input.Entry{{Ref: oldImage, Contract: "WorkingImage"}, {Ref: style, Contract: "StyleV2"}}
	if err := scope.QualifyArchivedResolvedScope(context.Background(), nil, old, children); err != nil {
		t.Fatal(err)
	}
	foreign := append([]v2input.Entry(nil), children...)
	foreign[1].Contract = "Stats20"
	if err := scope.QualifyArchivedResolvedScope(context.Background(), nil, old, foreign); err == nil {
		t.Fatal("archive contract relabel accepted")
	}
	if err := scope.QualifyArchivedResolvedScope(context.Background(), nil, target, children); err == nil {
		t.Fatal("target passed as archived binding")
	}
	if err := scope.QualifyArchivedResolvedScope(context.Background(), nil, old, children[1:]); err == nil {
		t.Fatal("missing source child accepted")
	}
}
