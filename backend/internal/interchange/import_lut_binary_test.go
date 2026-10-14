package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestImportLUTBinaryRequiresNativeProbeBeforePublication(t *testing.T) {
	image := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("image"))}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: image, CanvasHash: rawSHA([]byte("canvas"))}
	target := old
	target.AssetVersionID = uuid.New()
	target.Source.ArtifactID = uuid.New()
	array := []byte("lut payload for mocked native probe")
	archived := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(array)}
	staged := v2input.Ref{ArtifactID: uuid.New(), SHA256: archived.SHA256}
	parentRaw, err := canonical.Marshal(map[string]any{"params": map[string]any{"image": image}, "absolute_lut": map[string]any{"artifact": archived, "size": 33, "dtype": "float32", "order": "RGB_C", "profile_id": "work_rec2020_gamma24_v1"}})
	if err != nil {
		t.Fatal(err)
	}
	parentStaged := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(parentRaw)}
	parsed := importParsedValidatorFixture()
	for _, item := range []struct {
		original, staged v2input.Ref
		path, kind       string
	}{{v2input.Ref{ArtifactID: uuid.New(), SHA256: parentStaged.SHA256}, parentStaged, "documents/resolved.json", "ResolvedStyle"}, {archived, staged, "arrays/lut.npy", "AbsoluteLUTArray"}} {
		parsed.Manifest.Inventory = append(parsed.Manifest.Inventory, importBundleInventory{ID: item.original.ArtifactID, SHA: item.original.SHA256, Path: item.path, Contract: item.kind})
		parsed.Files = append(parsed.Files, importParsedFile{Path: item.path, Content: item.staged, Contract: item.kind})
	}
	parsedRaw, err := canonical.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	runtime := rawSHA([]byte("runtime"))
	probe := &exportArrayProbeStub{runtime: runtime, err: errors.New("invalid LUT NPY")}
	scratch, err := NewExportArchiveScratch(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &importBinaryPublisherStub{}
	remapper := ImportLUTBinaryRemapper{Workflow: uuid.New(), Parsed: parsedRaw, Reader: remapArchiveStub{parentStaged.ArtifactID: parentRaw}, Resolved: &resolvedQualifierStub{}, Catalog: &importCatalogStub{object: objectstore.Object{Key: "lut", VersionID: "v1", SHA256: archived.SHA256, Bytes: int64(len(array)), ContentType: "application/octet-stream"}}, Objects: &importReaderStub{body: array}, Scratch: scratch, Probe: probe, RuntimeSHA: runtime, Publisher: publisher}
	_, _, err = remapper.RemapImportBinary(context.Background(), uuid.New(), archived, staged, "AbsoluteLUTArray", old, target, 1<<20)
	if !errors.Is(err, probe.err) || publisher.calls != 0 || probe.calls != 1 {
		t.Fatal("native failure reached publisher", err)
	}
	probe.err = nil
	ref, size, err := remapper.RemapImportBinary(context.Background(), uuid.New(), archived, staged, "AbsoluteLUTArray", old, target, 1<<20)
	if err != nil || !validImportRef(ref) || size != int64(len(array)) || publisher.calls != 1 {
		t.Fatal("qualified LUT was not published", err)
	}
}
