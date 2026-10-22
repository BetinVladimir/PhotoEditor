package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// AuthoredDetailTransformDocument contains physically read immutable documents.
// Its content is not an adopted-producer capability: publication independently
// checks the separate transform task and canonical geometry authority in SQL.
type AuthoredDetailTransformDocument struct {
	Root, Target []byte
	TargetRef    v2input.Ref
}

// CompileAuthoredDetailOccurrence preserves every authored region/decision and
// style reference. Only image and the exact mask paths to the current canvas are
// materialized. Fresh selected faces and skin cannot enter this interface.
func CompileAuthoredDetailOccurrence(original, current []byte, currentRef v2input.Ref, documents []AuthoredDetailTransformDocument) ([]byte, error) {
	fail := func() ([]byte, error) { return nil, v2input.ErrInvalid }
	if !validLayerArtifactRef(currentRef) || len(documents) > 32896 {
		return fail()
	}
	_, err := review.ParseWorkingImage(current)
	if err != nil {
		return fail()
	}
	if !authoredDetailCanonical(current) || authoredDetailSHA(current) != currentRef.SHA256 {
		return fail()
	}
	var body map[string]json.RawMessage
	if !authoredDetailCanonical(original) || json.Unmarshal(original, &body) != nil || len(body) != 4 {
		return fail()
	}
	for _, key := range []string{"image", "resolved", "regions", "protected"} {
		if _, ok := body[key]; !ok {
			return fail()
		}
	}
	for _, key := range []string{"image", "resolved"} {
		var ref v2input.Ref
		if json.Unmarshal(body[key], &ref) != nil || !validLayerArtifactRef(ref) {
			return fail()
		}
	}
	var regions []map[string]json.RawMessage
	var protections []json.RawMessage
	if json.Unmarshal(body["regions"], &regions) != nil || regions == nil || len(regions) > 128 || json.Unmarshal(body["protected"], &protections) != nil || protections == nil || len(protections) > 32 {
		return fail()
	}
	masks := make([]json.RawMessage, 0, len(regions)+len(protections))
	for _, region := range regions {
		masks = append(masks, region["mask"])
	}
	masks = append(masks, protections...)
	projected, used, err := compileAuthoredMaskPaths(masks, current, currentRef, documents)
	if err != nil || len(used) != len(documents) {
		return fail()
	}
	for i, region := range regions {
		region["mask"] = projected[i]
	}
	copy(protections, projected[len(regions):])
	body["image"], err = canonical.Marshal(currentRef)
	if err != nil {
		return fail()
	}
	body["regions"], err = canonical.Marshal(regions)
	if err != nil {
		return fail()
	}
	body["protected"], err = canonical.Marshal(protections)
	if err != nil {
		return fail()
	}
	return canonical.Marshal(body)
}

func authoredDetailCanonical(raw []byte) bool {
	if len(raw) < 2 || len(raw) > 1048576 {
		return false
	}
	b, e := canonical.Bytes(raw)
	return e == nil && bytes.Equal(b, raw)
}
func authoredDetailSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// compileAuthoredMaskPaths uses only independently bound transform documents.
// Returned indices let the owning caller partition exact witnesses for region,
// protection and layer-expression masks without accepting unused documents.
func compileAuthoredMaskPaths(masks []json.RawMessage, current []byte, currentRef v2input.Ref, documents []AuthoredDetailTransformDocument) ([]json.RawMessage, []int, error) {
	fail := func() ([]json.RawMessage, []int, error) { return nil, nil, v2input.ErrInvalid }
	if !validLayerArtifactRef(currentRef) || !authoredDetailCanonical(current) || authoredDetailSHA(current) != currentRef.SHA256 || len(documents) > 32896 {
		return fail()
	}
	target, err := review.ParseWorkingImage(current)
	if err != nil {
		return fail()
	}
	type edge struct{ from, to review.MaskRef }
	edges := make([]edge, 0, len(documents))
	used := map[int]bool{}
	for _, document := range documents {
		if !validLayerArtifactRef(document.TargetRef) || !authoredDetailCanonical(document.Root) || !authoredDetailCanonical(document.Target) || authoredDetailSHA(document.Target) != document.TargetRef.SHA256 {
			return fail()
		}
		guide, err := review.ParseWorkingImage(document.Target)
		if err != nil || guide.Color != target.Color {
			return fail()
		}
		var root struct {
			Params struct {
				Mask    json.RawMessage   `json:"mask"`
				Target  v2input.Ref       `json:"target"`
				Step    json.RawMessage   `json:"step"`
				DType   string            `json:"dtype"`
				Regions []json.RawMessage `json:"regions"`
			} `json:"params"`
			Mask    json.RawMessage   `json:"mask"`
			DType   string            `json:"dtype"`
			Radius  *int              `json:"guided_radius"`
			Regions []json.RawMessage `json:"regions"`
		}
		if json.Unmarshal(document.Root, &root) != nil || root.Params.Target != document.TargetRef || root.DType != "float32" || root.Params.DType != "float32" || root.Radius == nil || *root.Radius < 1 || len(root.Regions) != 0 || len(root.Params.Regions) != 0 {
			return fail()
		}
		from, err := review.ParseMaskRef(root.Params.Mask)
		if err != nil {
			return fail()
		}
		to, err := review.ParseTransformedMask(document.Root)
		if err != nil {
			return fail()
		}
		if to.Canvas != guide.Canvas || !bytes.Equal(to.TransformChain, guide.TransformChain) || to.Artifact == from.Artifact {
			return fail()
		}
		retained := from
		retained.Artifact = to.Artifact
		retained.Canvas = guide.Canvas
		retained.TransformChain = append(json.RawMessage(nil), guide.TransformChain...)
		a, _ := canonical.Marshal(retained)
		b, _ := canonical.Marshal(to)
		if !bytes.Equal(a, b) {
			return fail()
		}
		var step struct {
			From review.Canvas `json:"from_canvas"`
			To   review.Canvas `json:"to_canvas"`
		}
		if json.Unmarshal(root.Params.Step, &step) != nil || step.From != from.Canvas || step.To != guide.Canvas {
			return fail()
		}
		var chain struct {
			Steps []json.RawMessage `json:"steps"`
			Hash  string            `json:"hash"`
		}
		if json.Unmarshal(from.TransformChain, &chain) != nil || len(chain.Steps) >= 128 {
			return fail()
		}
		chain.Steps = append(chain.Steps, root.Params.Step)
		steps, err := canonical.Marshal(map[string]any{"steps": chain.Steps})
		if err != nil {
			return fail()
		}
		chain.Hash = authoredDetailSHA(steps)
		expected, err := canonical.Marshal(chain)
		if err != nil || !bytes.Equal(expected, guide.TransformChain) {
			return fail()
		}
		edges = append(edges, edge{from, to})
	}
	project := func(maskRaw json.RawMessage) (json.RawMessage, error) {
		mask, err := review.ParseMaskRef(maskRaw)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for mask.Canvas != target.Canvas || !bytes.Equal(mask.TransformChain, target.TransformChain) {
			key, _ := canonical.Marshal(mask)
			if seen[string(key)] {
				return nil, v2input.ErrInvalid
			}
			seen[string(key)] = true
			found := -1
			for i, e := range edges {
				raw, _ := canonical.Marshal(e.from)
				if bytes.Equal(raw, key) {
					if found != -1 {
						return nil, v2input.ErrInvalid
					}
					found = i
				}
			}
			if found < 0 {
				return nil, v2input.ErrInvalid
			}
			used[found] = true
			mask = edges[found].to
		}
		return canonical.Marshal(mask)
	}

	out := make([]json.RawMessage, 0, len(masks))
	for _, mask := range masks {
		value, err := project(mask)
		if err != nil {
			return fail()
		}
		out = append(out, value)
	}
	indices := make([]int, 0, len(used))
	for i := range documents {
		if used[i] {
			indices = append(indices, i)
		}
	}
	return out, indices, nil
}
