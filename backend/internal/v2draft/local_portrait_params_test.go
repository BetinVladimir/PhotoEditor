package v2draft

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"strings"
	"testing"
)

func TestLocalPortraitDocumentRefsRemainTyped(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	source, mask, guard, parent := ref(), ref(), ref(), ref()
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: strings.Repeat("b", 64)}
	hair := v2portrait.HairParams{SchemaVersion: 1, Base: base, Strength: .5, Mask: mask, Protected: guard, SaturationFactor: 1}
	raw, _, err := hair.Compile()
	if err != nil {
		t.Fatal(err)
	}
	children, err := DocumentChildren(raw, "Params", parent)
	if err != nil || len(children) != 3 {
		t.Fatal(children, err)
	}
	for i, child := range children {
		expected := []v2input.Ref{source, mask, guard}[i]
		contract := []string{"WorkingImage", "MaskRef", "MaskRef"}[i]
		if child.Ref != expected || child.Contract != contract || child.ParentRef != parent || child.CanvasHash != base.CanvasHash {
			t.Fatal("typed occurrence lost", child)
		}
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("decode")
	}
	for _, key := range []string{"detail", "base", "strength", "protected"} {
		damaged := map[string]any{}
		for k, v := range fields {
			damaged[k] = v
		}
		delete(damaged, key)
		body, _ := canonical.Marshal(damaged)
		if _, err := DocumentChildren(body, "Params", parent); err == nil {
			t.Fatal("missing field accepted", key)
		}
	}
	fields["extra"] = true
	body, _ := canonical.Marshal(fields)
	if _, err := DocumentChildren(body, "Params", parent); err == nil {
		t.Fatal("unknown field accepted")
	}
}
