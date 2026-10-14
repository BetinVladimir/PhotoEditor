package v2draft

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func TestLocalPortraitClientSelectionExactAndUnprivileged(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	mask, guard, target, protected := ref(), ref(), ref(), ref()
	for _, tc := range []struct {
		operation string
		params    any
	}{
		{"portrait.hair", v2portrait.HairParams{SchemaVersion: 1, Base: base, Strength: .5, Mask: mask, Protected: guard, HueDeltaDegrees: 10, SaturationFactor: 1.1, Detail: .3}},
		{"portrait.clothing", v2portrait.ClothingParams{SchemaVersion: 1, Base: base, Strength: .5, Mask: mask, Protected: guard, HueDeltaDegrees: 10, SaturationFactor: 1.1, WrinkleStrength: .3}},
	} {
		params, err := canonical.Marshal(tc.params)
		if err != nil {
			t.Fatal(err)
		}
		request := LocalPortraitClientSelection{SchemaVersion: 1, Snapshot: uuid.New(), Ordinal: 0, Operation: tc.operation, Params: params, TargetPixel: target, ProtectedPixel: protected}
		raw, err := canonical.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeLocalPortraitClientSelection(raw); err != nil {
			t.Fatalf("%s: %v", tc.operation, err)
		}
		request.ProtectedPixel = request.TargetPixel
		bad, err := canonical.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeLocalPortraitClientSelection(bad); err == nil {
			t.Fatalf("%s accepted duplicate physical ref", tc.operation)
		}
	}
	params, _ := canonical.Marshal(v2portrait.HairParams{SchemaVersion: 1, Base: base, Strength: .5, Mask: mask, Protected: guard, HueDeltaDegrees: 10, SaturationFactor: 1.1, Detail: .3})
	raw, _ := canonical.Marshal(map[string]any{"schema_version": 1, "snapshot_id": uuid.New(), "ordinal": 0, "operation": "portrait.hair", "params": json.RawMessage(params), "target_pixel": target, "protected_pixel": protected, "runtime_sha": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"})
	if _, err := DecodeLocalPortraitClientSelection(raw); err == nil {
		t.Fatal("accepted client-owned runtime pin")
	}
}
