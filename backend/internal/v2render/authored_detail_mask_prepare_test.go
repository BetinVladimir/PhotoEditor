package v2render

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredMaskPreparationUsesOnlyNextActualGeometry(t *testing.T) {
	v, _ := detailMaterializationFixture(t)
	var original struct {
		Regions []struct {
			Mask json.RawMessage `json:"mask"`
		} `json:"regions"`
	}
	if err := json.Unmarshal([]byte(v.Original), &original); err != nil {
		t.Fatal(err)
	}
	mask, err := review.ParseMaskRef(original.Regions[0].Mask)
	if err != nil {
		t.Fatal(err)
	}
	target, err := review.ParseWorkingImage([]byte(v.Current))
	if err != nil {
		t.Fatal(err)
	}
	if next, err := authoredMaskNextGeometry(mask, target); err != nil || !next {
		t.Fatalf("real next geometry refused: %v", err)
	}
	current := mask
	current.Canvas = target.Canvas
	current.TransformChain = target.TransformChain
	if next, err := authoredMaskNextGeometry(current, target); err != nil || next {
		t.Fatalf("same geometry rerun: %v", err)
	}
	var chain map[string]json.RawMessage
	json.Unmarshal(target.TransformChain, &chain)
	var steps []json.RawMessage
	json.Unmarshal(chain["steps"], &steps)
	chain["steps"], _ = canonical.Marshal(append(steps, steps[0]))
	target.TransformChain, _ = canonical.Marshal(chain)
	if _, err := authoredMaskNextGeometry(mask, target); err == nil {
		t.Fatal("two-step jump accepted")
	}
	target, _ = review.ParseWorkingImage([]byte(v.Current))
	target.Canvas.Width++
	if _, err := authoredMaskNextGeometry(mask, target); err == nil {
		t.Fatal("foreign target canvas accepted")
	}
}
func TestAuthoredMaskPreparationCollectsExactFiveDSLOps(t *testing.T) {
	a := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	b := a
	b.ArtifactID = uuid.New()
	ref := func(r v2input.Ref) any { return map[string]any{"op": "ref", "mask": r} }
	raw, _ := canonical.Marshal(map[string]any{"op": "subtract", "left": map[string]any{"op": "union", "left": ref(a), "right": map[string]any{"op": "invert", "operand": ref(b)}}, "right": map[string]any{"op": "intersect", "left": ref(a), "right": ref(b)}})
	refs, err := authoredDetailExpressionRefs(context.Background(), raw)
	if err != nil || len(refs) != 2 || refs[0] != a || refs[1] != b {
		t.Fatalf("exact expression order/dedup: %v %v", refs, err)
	}
	for _, bad := range []string{`{"op":"ref","mask":null}`, `{"op":"invert","operand":null}`, `{"op":"union","left":null,"right":null}`, `{"op":"constant","value":1}`, `{"op":"ref","mask":{},"extra":1}`} {
		if _, err := authoredDetailExpressionRefs(context.Background(), []byte(bad)); err == nil {
			t.Fatalf("malformed expression accepted: %s", bad)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authoredDetailExpressionRefs(ctx, raw); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
	if _, err := (SQLAuthoredDetailMaskPreparation{}).Resume(ctx, auth.Identity{}, uuid.New(), uuid.New(), 2, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel crossed database: %v", err)
	}
}

func TestAuthoredDetailNoopRetainsOccurrenceWithoutProviderInputs(t *testing.T) {
	id := uuid.New()
	source := AuthoredInstructionSource{Instruction: AuthoredGeometryInstruction{Layers: []uuid.UUID{id}}}
	for _, v := range []struct {
		enabled bool
		opacity float64
		active  bool
	}{{true, 1, true}, {false, 1, false}, {true, 0, false}, {true, 0.5, true}} {
		raw, _ := canonical.Marshal(map[string]any{"layers": []any{map[string]any{"id": id, "enabled": v.enabled, "opacity": v.opacity}}})
		source.LayerStackJCS = string(raw)
		active, err := authoredDetailOccurrenceActive(source)
		if err != nil || active != v.active {
			t.Fatalf("authored noop semantics changed: %v", err)
		}
	}
	raw, _ := canonical.Marshal(map[string]any{"layers": []any{map[string]any{"id": id, "enabled": true, "opacity": nil}}})
	source.LayerStackJCS = string(raw)
	if _, err := authoredDetailOccurrenceActive(source); err == nil {
		t.Fatal("malformed opacity treated as noop")
	}
}
func TestAuthoredDetailConstructorDoesNotInventQualification(t *testing.T) {
	if _, err := NewSQLAuthoredDetailMaskPreparation(nil, nil, nil, "", nil, 1073741824, 1, 1); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("missing concrete qualification/catalog accepted")
	}
}
