package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestImportArchivedLUTGeometryRequiresExactQualifiedParent(t *testing.T) {
	image := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("image"))}
	lut := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("lut"))}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: image, CanvasHash: rawSHA([]byte("canvas"))}
	parentRaw, err := canonical.Marshal(map[string]any{"params": map[string]any{"image": image}, "absolute_lut": map[string]any{"artifact": lut, "size": 33, "dtype": "float32", "order": "RGB_C", "profile_id": "work_rec2020_gamma24_v1"}})
	if err != nil {
		t.Fatal(err)
	}
	parsed := importParsedValidatorFixture()
	staged := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(parentRaw)}
	parsed.Manifest.Inventory = append(parsed.Manifest.Inventory, importBundleInventory{ID: uuid.New(), Path: "documents/resolved.json", SHA: staged.SHA256, Contract: "ResolvedStyle"})
	parsed.Files = append(parsed.Files, importParsedFile{Path: "documents/resolved.json", Content: staged, Contract: "ResolvedStyle"})
	parsedRaw, err := canonical.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	qualifier := &resolvedQualifierStub{}
	source := ImportArchivedLUTGeometrySource{Parsed: parsedRaw, Reader: remapArchiveStub{staged.ArtifactID: parentRaw}, Qualifier: qualifier, Old: binding}
	entry := v2input.Entry{Ref: lut, Contract: "AbsoluteLUTArray"}
	geometry, err := source.ResolveExportArrayGeometry(context.Background(), entry)
	if err != nil || geometry.Width != 33 || geometry.Height != 33 || geometry.DType != "float32" || qualifier.archived != 1 {
		t.Fatal("qualified LUT occurrence missing", geometry, err)
	}
	foreign := entry
	foreign.Ref.SHA256 = rawSHA([]byte("foreign"))
	if _, err := source.ResolveExportArrayGeometry(context.Background(), foreign); err == nil {
		t.Fatal("same UUID with foreign SHA accepted")
	}
	other := source
	other.Old.Source.ArtifactID = uuid.New()
	if _, err := other.ResolveExportArrayGeometry(context.Background(), entry); err == nil {
		t.Fatal("foreign source accepted")
	}
}
