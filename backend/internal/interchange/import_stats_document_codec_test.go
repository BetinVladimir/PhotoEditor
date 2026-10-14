package interchange

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestImportStatsDocumentCodecRebindsOnlySourceImage(t *testing.T) {
	oldSource := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("old"))}
	newSource := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("new"))}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: oldSource, CanvasHash: rawSHA([]byte("canvas"))}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: newSource, CanvasHash: old.CanvasHash}
	value := map[string]any{"source_image": oldSource, "feature_version": "stats20_v1", "values": make([]float64, 20), "coverage_bins": make([]bool, 512), "mask_confidence": 0.7, "warnings": []string{"limited_coverage"}}
	raw, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := (ImportStatsDocumentCodec{}).RemapImportDocument(context.Background(), "Stats20", raw, old, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(prepared, &after); err != nil {
		t.Fatal(err)
	}
	for key, body := range before {
		if key != "source_image" && string(body) != string(after[key]) {
			t.Fatal("measurement changed", key)
		}
	}
	var rebound v2input.Ref
	if err := json.Unmarshal(after["source_image"], &rebound); err != nil || rebound != newSource {
		t.Fatal("source not rebound", err)
	}
	if _, err := (ImportStatsDocumentCodec{}).RemapImportDocument(context.Background(), "Stats20", raw, target, old, nil); err == nil {
		t.Fatal("foreign archived source accepted")
	}
}
