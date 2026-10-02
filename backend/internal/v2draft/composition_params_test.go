package v2draft

import (
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestCompositionParamsPreserveTypedRootsAndRejectBrokenExpression(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	source, proposal, mask, parent := ref(), ref(), ref(), ref()
	canvas := strings.Repeat("b", 64)
	leaf := map[string]any{"op": "ref", "mask": mask, "canvas_hash": canvas}
	body := map[string]any{"schema_version": 1, "base": v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: canvas}, "source_result": nil, "proposal_result": proposal, "mask": map[string]any{"op": "invert", "operand": leaf}, "protected_mask": mask, "opacity": .5, "blend": "normal"}
	raw, _ := canonical.Marshal(body)
	children, handled, err := compositionParamRefs(raw, "Params", parent)
	if err != nil || !handled || len(children) != 4 {
		t.Fatal("typed params roots", err, handled, len(children))
	}
	want := map[v2input.Ref]string{source: "WorkingImage", proposal: "ImageResult", mask: "MaskRef"}
	for _, child := range children {
		if child.Contract != want[child.Ref] || child.ParentRef != parent {
			t.Fatal("root contract lost", child)
		}
	}
	body["mask"] = map[string]any{"op": "ref", "mask": mask, "canvas_hash": strings.Repeat("c", 64)}
	raw, _ = canonical.Marshal(body)
	if _, handled, err := compositionParamRefs(raw, "Params", parent); !handled || !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("foreign canvas accepted", err)
	}
	var deep any = leaf
	for i := 0; i < 17; i++ {
		deep = map[string]any{"op": "invert", "operand": deep}
	}
	body["mask"] = deep
	raw, _ = canonical.Marshal(body)
	if _, _, err := compositionParamRefs(raw, "Params", parent); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("unbounded mask depth", err)
	}
	if _, handled, err := compositionParamRefs([]byte(`{"strength":0.5}`), "Params", parent); handled || err != nil {
		t.Fatal("unrelated params intercepted", err)
	}
}
