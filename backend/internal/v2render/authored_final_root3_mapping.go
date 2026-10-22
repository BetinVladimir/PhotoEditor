package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type finalRoot3MaskPair struct {
	Original v2input.Ref `json:"original"`
	Current  v2input.Ref `json:"current"`
}
type finalRoot3Occurrence struct {
	Path     []string    `json:"path"`
	Original v2input.Ref `json:"original"`
}
type finalRoot3Mapping struct {
	Ordinal       int                           `json:"ordinal"`
	Predecessor   string                        `json:"predecessor_sha256"`
	Receipts      []v2input.Ref                 `json:"phase_receipts"`
	Tenant        uuid.UUID                     `json:"tenant_id"`
	Parent        uuid.UUID                     `json:"parent_run_id"`
	Instruction   uuid.UUID                     `json:"instruction_id"`
	ProgramSHA    string                        `json:"program_sha256"`
	Recipe        v2input.Ref                   `json:"recipe"`
	Source        v2input.SessionVersionBinding `json:"source"`
	Run           uuid.UUID                     `json:"producer_run_id"`
	Task          uuid.UUID                     `json:"producer_task_id"`
	Attempt       int                           `json:"attempt"`
	Fence         int64                         `json:"fence"`
	Native        json.RawMessage               `json:"native"`
	Target        v2input.Ref                   `json:"target_image"`
	Canvas        json.RawMessage               `json:"target_canvas"`
	Chain         json.RawMessage               `json:"target_chain"`
	Global        *finalRoot3MaskPair           `json:"global_protection"`
	ExpressionSHA string                        `json:"local_expression_sha256"`
	Occurrences   []finalRoot3Occurrence        `json:"local_occurrences"`
	Leaves        []struct {
		Original v2input.Ref `json:"original"`
		SHA      string      `json:"pixels_sha256"`
		Bytes    int64       `json:"bytes"`
	} `json:"local_masks"`
	Layer      v2input.Ref  `json:"layer_mask"`
	Protection *v2input.Ref `json:"local_protection"`
}

func decodeAuthoredRoot3Mapping(raw []byte, ref v2input.Ref, out any) error {
	if _, e := v2draft.DocumentChildren(raw, "AuthoredGeometryMaskMappingV2", ref); e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return v2input.ErrInvalid
	}
	if json.Unmarshal(raw, out) != nil {
		return v2input.ErrInvalid
	}
	return nil
}
func finalRoot3Document(d AuthoredFinalDocuments, ref v2input.Ref) ([]byte, error) {
	raw, ok := d.Bodies[ref]
	if !ok || !validLayerArtifactRef(ref) || len(raw) < 2 || len(raw) > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	sum := sha256.Sum256(raw)
	wire, e := canonical.Bytes(raw)
	if e != nil || !bytes.Equal(wire, raw) || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	return raw, nil
}

// Metadata binding after actual root3 publication authority and physical reads.
// This does not prove the leaf computations. Final native/mask replay remains
// independently mandatory before canonicalization under its actual CPU/GPU gate.
func verifyAuthoredFinalRoot3Mapping(d AuthoredFinalDocuments, phase AuthoredPhaseReceipt, rootRaw []byte, original v2input.SessionVersionBinding, prior []v2input.Ref) error {
	bad := func() error { return v2input.ErrInvalid }
	rootSum := sha256.Sum256(rootRaw)
	if hex.EncodeToString(rootSum[:]) != phase.Result.SHA256 {
		return bad()
	}
	s := d.Inventory.Context
	if _, e := v2draft.DocumentChildren(rootRaw, "AuthoredGeometryPhaseResultV3", phase.Result); e != nil {
		return e
	}
	var root struct {
		Mapping    v2input.Ref     `json:"mask_mapping"`
		Registered v2input.Ref     `json:"registered_original"`
		Native     json.RawMessage `json:"native"`
		Blend      struct {
			Layer      uuid.UUID    `json:"layer_id"`
			Operation  string       `json:"operation"`
			Baseline   v2input.Ref  `json:"baseline"`
			Params     v2input.Ref  `json:"params"`
			Opacity    float64      `json:"opacity"`
			Blend      string       `json:"blend"`
			Mask       v2input.Ref  `json:"target_mask"`
			Protection *v2input.Ref `json:"target_protection"`
		} `json:"blend"`
	}
	if json.Unmarshal(rootRaw, &root) != nil {
		return bad()
	}
	raw, e := finalRoot3Document(d, root.Mapping)
	if e != nil {
		return e
	}
	var m finalRoot3Mapping
	if decodeAuthoredRoot3Mapping(raw, root.Mapping, &m) != nil {
		return bad()
	}
	if m.Ordinal != phase.Ordinal || m.Predecessor != phase.PredecessorSHA || m.Tenant != phase.Tenant || m.Parent != phase.Parent || m.Instruction != phase.Instruction || m.ProgramSHA != phase.ProgramSHA || m.Recipe != phase.OriginalRecipe || m.Source != phase.InputSource || m.Run != phase.RunID || m.Task != phase.TaskID || m.Attempt != phase.Attempt || m.Fence != phase.Fence || m.Target != phase.OutputSource.Source || m.Layer != root.Blend.Mask {
		return bad()
	}
	equal := func(a, b any) bool {
		x, e := canonical.Marshal(a)
		y, f := canonical.Marshal(b)
		return e == nil && f == nil && bytes.Equal(x, y)
	}
	if !equal(m.Receipts, prior) || !equal(m.Native, root.Native) || !equal(m.Protection, root.Blend.Protection) {
		return bad()
	}
	retained := map[v2input.Ref]bool{}
	for _, r := range phase.PhysicalClosure {
		retained[r] = true
	}
	if !retained[root.Mapping] || !retained[root.Registered] || !retained[root.Blend.Baseline] || !retained[m.Layer] || m.Protection != nil && !retained[*m.Protection] || m.Global != nil && !retained[m.Global.Current] {
		return bad()
	}
	targetRaw, e := finalRoot3Document(d, m.Target)
	if e != nil {
		return e
	}
	target, e := review.ParseWorkingImage(targetRaw)
	if e != nil {
		return e
	}
	var image map[string]json.RawMessage
	if json.Unmarshal(targetRaw, &image) != nil || !equal(m.Canvas, image["canvas"]) || !equal(m.Chain, image["transform_chain"]) {
		return bad()
	}
	for _, ref := range []v2input.Ref{root.Registered, root.Blend.Baseline} {
		raw, err := finalRoot3Document(d, ref)
		if err != nil {
			return err
		}
		image, err := review.ParseWorkingImage(raw)
		if err != nil || image.Canvas != target.Canvas || !equal(image.TransformChain, target.TransformChain) || image.Color != target.Color {
			return bad()
		}
	}
	canvas, e := canonical.Hash(target.Canvas)
	if e != nil || canvas != phase.OutputSource.CanvasHash {
		return bad()
	}
	originalRaw, e := finalRoot3Document(d, original.Source)
	if e != nil {
		return e
	}
	recipe, e := CompileRenderEvidence(original, s.OriginalRecipe, []byte(s.RecipeJCS), originalRaw)
	if e != nil {
		return e
	}
	var authored struct {
		Stack     *v2input.Ref `json:"layer_stack"`
		Protected *v2input.Ref `json:"protected_mask"`
	}
	if json.Unmarshal(recipe.Recipe.Recipe, &authored) != nil || authored.Stack == nil {
		return bad()
	}
	layerRaw := []byte(s.LayerStackJCS)
	sum := sha256.Sum256(layerRaw)
	if hex.EncodeToString(sum[:]) != authored.Stack.SHA256 {
		return bad()
	}
	plan, e := Compile(layerRaw, original)
	if e != nil || len(plan.Layers)+1 != s.Instruction.Ordinal || phase.Ordinal < 1 || phase.Ordinal > len(plan.Layers) {
		return bad()
	}
	layer := plan.Layers[phase.Ordinal-1].Layer
	if !layer.Active() || !authoredGeometryOperation(layer.Operation) || layer.OperationVersion != "1" || layer.ID != root.Blend.Layer || layer.Operation != root.Blend.Operation || layer.Params != root.Blend.Params || layer.Opacity != root.Blend.Opacity || layer.Blend != root.Blend.Blend {
		return bad()
	}
	expression, e := canonical.Hash(layer.Mask)
	if e != nil || expression != m.ExpressionSHA {
		return bad()
	}
	occurrences := []finalRoot3Occurrence{}
	unique := []v2input.Ref{}
	seen := map[uuid.UUID]v2input.Ref{}
	var walk func(json.RawMessage, []string) error
	walk = func(raw json.RawMessage, path []string) error {
		var x struct {
			Op      string          `json:"op"`
			Mask    *v2input.Ref    `json:"mask"`
			Left    json.RawMessage `json:"left"`
			Right   json.RawMessage `json:"right"`
			Operand json.RawMessage `json:"operand"`
		}
		if json.Unmarshal(raw, &x) != nil {
			return bad()
		}
		child := func(name string, v json.RawMessage) error {
			p := append(append([]string{}, path...), name)
			return walk(v, p)
		}
		switch x.Op {
		case "ref":
			if x.Mask == nil {
				return bad()
			}
			occurrences = append(occurrences, finalRoot3Occurrence{Path: append([]string{}, path...), Original: *x.Mask})
			if old, ok := seen[x.Mask.ArtifactID]; ok {
				if old != *x.Mask {
					return bad()
				}
			} else {
				seen[x.Mask.ArtifactID] = *x.Mask
				unique = append(unique, *x.Mask)
			}
		case "invert":
			return child("operand", x.Operand)
		case "union", "intersect", "subtract":
			if e := child("left", x.Left); e != nil {
				return e
			}
			return child("right", x.Right)
		default:
			return bad()
		}
		return nil
	}
	if walk(layer.Mask, []string{}) != nil || !equal(occurrences, m.Occurrences) || len(unique) != len(m.Leaves) {
		return bad()
	}
	for i, ref := range unique {
		if m.Leaves[i].Original != ref {
			return bad()
		}
	}
	if authored.Protected == nil {
		if m.Global != nil || m.Protection != nil {
			return bad()
		}
	} else {
		if m.Global == nil || m.Global.Original != *authored.Protected || m.Protection == nil {
			return bad()
		}
		globalRaw, e := finalRoot3Document(d, m.Global.Current)
		if e != nil {
			return e
		}
		global, e := review.ParseMaskRef(globalRaw)
		if e != nil {
			return e
		}
		unionRaw, e := finalRoot3Document(d, *m.Protection)
		if e != nil {
			return e
		}
		union, e := review.ParseMaskRef(unionRaw)
		if e != nil {
			return e
		}
		// For the closed five geometry operations the union is exactly the immutable
		// global protection, under distinct own output identities; never layer mask.
		if global.Artifact.SHA256 != union.Artifact.SHA256 || global.Canvas != target.Canvas || union.Canvas != target.Canvas || !equal(global.TransformChain, target.TransformChain) || !equal(union.TransformChain, target.TransformChain) || !global.Protected || !union.Protected {
			return bad()
		}
	}
	maskRaw, e := finalRoot3Document(d, m.Layer)
	if e != nil {
		return e
	}
	mask, e := review.ParseMaskRef(maskRaw)
	if e != nil {
		return e
	}
	if mask.Canvas != target.Canvas || !equal(mask.TransformChain, target.TransformChain) {
		return bad()
	}
	return nil
}
