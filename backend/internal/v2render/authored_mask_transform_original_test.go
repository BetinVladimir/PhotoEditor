package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestAuthoredMaskOriginalChainPrefix(t *testing.T) {
	a := []json.RawMessage{json.RawMessage(`{"kind":"crop","x":1}`)}
	for _, tc := range []struct {
		name    string
		current []json.RawMessage
		want    bool
	}{
		{"exact", a, true}, {"append", append(append([]json.RawMessage{}, a...), json.RawMessage(`{"kind":"resize"}`)), true},
		{"same length foreign", []json.RawMessage{json.RawMessage(`{"kind":"crop","x":2}`)}, false},
		{"shortened", nil, false}, {"foreign prefix", []json.RawMessage{json.RawMessage(`{"kind":"resize"}`), a[0]}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if authoredMaskChainPrefix(a, tc.current) != tc.want {
				t.Fatal("wrong prefix decision")
			}
		})
	}
}

func authoredOriginalMaskFixture(t *testing.T) (AuthoredInstructionSource, AuthoredMaskTransformDescriptor, v2input.Ref, string) {
	t.Helper()
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	chain, _ := canonical.Marshal(map[string]any{"steps": []any{}})
	chainSHA, _ := canonical.Hash(json.RawMessage(chain))
	m := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}, Canvas: review.Canvas{Width: 8, Height: 8, Orientation: 1, SpaceID: "working", SourceSHA256: strings.Repeat("c", 64)}, TransformChain: json.RawMessage(`{"hash":"` + chainSHA + `","steps":[]}`), Selector: "manual", Confidence: 1, Protected: true}
	raw, err := canonical.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = review.ParseMaskRef(raw); err != nil {
		t.Fatal(err)
	}
	sha, _ := canonical.Hash(json.RawMessage(raw))
	maskRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("c", 64)}
	id := uuid.New()
	params := ref()
	stack, err := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{map[string]any{"id": id, "enabled": true, "opacity": 1, "blend": "normal", "operation": "portrait.detail", "operation_version": "1", "params": params, "mask": map[string]any{"op": "invert", "operand": map[string]any{"op": "ref", "mask": maskRef, "canvas_hash": binding.CanvasHash}}}}})
	if err != nil {
		t.Fatal(err)
	}
	s := AuthoredInstructionSource{LayerStackJCS: string(stack)}
	s.Instruction.Layers = []uuid.UUID{id}
	d := AuthoredMaskTransformDescriptor{SchemaVersion: 1, Parent: uuid.New(), Instruction: uuid.New(), OriginalMask: m, SourceMask: m, OriginalParams: params, Target: ref()}
	return s, d, maskRef, string(raw)
}

func TestAuthoredMaskOriginalStandaloneModes(t *testing.T) {
	s, d, maskRef, raw := authoredOriginalMaskFixture(t)
	d.OriginalLayerMaskRef = &maskRef
	d.OriginalLayerMaskJCS = &raw
	if err := validateAuthoredMaskOriginalWitness(s, d); err != nil {
		t.Fatal("actual selected DSL occurrence", err)
	}
	for _, name := range []string{"missing body", "foreign layer", "foreign params", "foreign ref", "mixed modes"} {
		t.Run(name, func(t *testing.T) {
			b := d
			c := s
			switch name {
			case "missing body":
				b.OriginalLayerMaskJCS = nil
			case "foreign layer":
				c.Instruction.Layers = []uuid.UUID{uuid.New()}
			case "foreign params":
				b.OriginalParams.ArtifactID = uuid.New()
			case "foreign ref":
				r := maskRef
				r.ArtifactID = uuid.New()
				b.OriginalLayerMaskRef = &r
			case "mixed modes":
				b.OriginalProtectedRef = &maskRef
				b.OriginalProtectedJCS = &raw
			}
			if validateAuthoredMaskOriginalWitness(c, b) == nil {
				t.Fatal("accepted foreign authority")
			}
		})
	}
	// Build an actual FrozenRenderRecipe, preserving its real protected_mask type.
	var stack struct {
		Source v2input.SessionVersionBinding `json:"source"`
	}
	if err := json.Unmarshal([]byte(s.LayerStackJCS), &stack); err != nil {
		t.Fatal(err)
	}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	stackSHA, _ := canonical.Hash(json.RawMessage(s.LayerStackJCS))
	stackRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: stackSHA}
	recipe, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": stack.Source, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": stackRef, "protected_mask": maskRef, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{}})
	frozen, err := v2draft.BuildFrozenRenderRecipe(recipe, v2draft.ResolvedRecipeInputs{Binding: stack.Source, BaseStyle: style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("e", 64), ModelLockSHA: strings.Repeat("f", 64)}, []byte(s.LayerStackJCS))
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := canonical.Hash(json.RawMessage(frozen))
	s.OriginalRecipe = v2input.Ref{ArtifactID: uuid.New(), SHA256: hash}
	s.RecipeJCS = string(frozen)
	d.OriginalLayerMaskRef = nil
	d.OriginalLayerMaskJCS = nil
	d.OriginalProtectedRef = &maskRef
	d.OriginalProtectedJCS = &raw
	d.OriginalParams = s.OriginalRecipe
	d.OriginalParamsJCS = s.RecipeJCS
	if err := validateAuthoredMaskOriginalWitness(s, d); err != nil {
		t.Fatal("real frozen recipe protection", err)
	}
	b := d
	b.OriginalParams.ArtifactID = uuid.New()
	if validateAuthoredMaskOriginalWitness(s, b) == nil {
		t.Fatal("recipe relabel accepted")
	}
}

func TestAuthoredMaskNamespaceSeparatesOriginalOccurrence(t *testing.T) {
	_, d, r, raw := authoredOriginalMaskFixture(t)
	inline, _, _, err := AuthoredMaskTransformIDs(d)
	if err != nil {
		t.Fatal(err)
	}
	d.OriginalLayerMaskRef = &r
	d.OriginalLayerMaskJCS = &raw
	dsl, _, _, err := AuthoredMaskTransformIDs(d)
	if err != nil {
		t.Fatal(err)
	}
	d.OriginalLayerMaskRef = nil
	d.OriginalLayerMaskJCS = nil
	d.OriginalProtectedRef = &r
	d.OriginalProtectedJCS = &raw
	global, _, _, err := AuthoredMaskTransformIDs(d)
	if err != nil {
		t.Fatal(err)
	}
	if inline == dsl || inline == global || dsl == global {
		t.Fatal("dual-use mask collided")
	}
	d.Previous = &AuthoredMaskTransformPrevious{} // runtime predecessor is not original occurrence identity
	again, _, _, err := AuthoredMaskTransformIDs(d)
	if err != nil || again != global {
		t.Fatal("runtime predecessor changed namespace", err)
	}
}
