package v2draft

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
)

// These content readers retain typed edges; current producer authority and
// independently recomputed leaf bytes remain mandatory at adoption.
func authoredGeometryRoot3Refs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, e := authoredObject(raw, "schema_version operation operation_version tenant_id parent_run_id instruction_id program_sha256 recipe original_source input_source native blend registered_original blended_result blended_image mask_mapping")
	if e != nil {
		return nil, e
	}
	if _, ok := authoredInt(f["schema_version"], 3, 3); !ok {
		return nil, v2input.ErrInvalid
	}
	n, e := authoredObject(f["native"], "run_id task_id result_id attempt fence operation operation_version params root image report")
	if e != nil {
		return nil, e
	}
	var b map[string]json.RawMessage
	if json.Unmarshal(f["blend"], &b) != nil {
		return nil, v2input.ErrInvalid
	}
	report, ok := layerRef(n["report"])
	registered, good := layerRef(b["registration_report"])
	baseline, bok := layerRef(b["baseline"])
	if !ok || !good || !bok || report != registered {
		return nil, v2input.ErrInvalid
	}
	for _, r := range []json.RawMessage{f["blended_image"], f["registered_original"], n["image"]} {
		ref, valid := layerRef(r)
		if !valid || ref.ArtifactID == baseline.ArtifactID {
			return nil, v2input.ErrInvalid
		}
	}
	mapping, ok := layerRef(f["mask_mapping"])
	if !ok {
		return nil, v2input.ErrInvalid
	}
	for _, key := range []string{"recipe", "registered_original", "blended_result", "blended_image"} {
		r, valid := layerRef(f[key])
		if !valid || r.ArtifactID == mapping.ArtifactID {
			return nil, v2input.ErrInvalid
		}
	}
	// Reuse every historical root1 structural check, changing only the explicit
	// version and required mapping edge. Historical root1 parsing is unchanged.
	delete(f, "mask_mapping")
	f["schema_version"] = json.RawMessage("1")
	projected, e := canonical.Marshal(f)
	if e != nil {
		return nil, e
	}
	children, e := authoredGeometryRefs(projected, path, parent)
	if e != nil {
		return nil, e
	}
	op, _ := authoredString(n["operation"])
	if op == "image.upscale" {
		for i := range children {
			if children[i].Path == path+".native.report" || children[i].Path == path+".blend.registration_report" {
				children[i].Contract = "UpscaleReport"
			}
		}
	}
	return append(children, LayerRef{Path: path + ".mask_mapping", ParentRef: parent, Ref: mapping, Contract: "AuthoredGeometryMaskMappingV2"}), nil
}

func authoredGeometryMapping2Refs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, e := authoredObject(raw, "schema_version ordinal predecessor_sha256 phase_receipts tenant_id parent_run_id instruction_id program_sha256 recipe source producer_run_id producer_task_id attempt fence native target_image target_canvas target_chain global_protection local_expression_sha256 local_occurrences local_masks layer_mask local_protection")
	if e != nil {
		return nil, e
	}
	bad := func() ([]LayerRef, error) { return nil, v2input.ErrInvalid }
	if _, ok := authoredInt(f["schema_version"], 2, 2); !ok {
		return bad()
	}
	ordinal, ok := authoredInt(f["ordinal"], 1, 4998)
	if !ok {
		return bad()
	}
	for _, k := range []string{"program_sha256", "predecessor_sha256", "local_expression_sha256"} {
		if !authoredSHA(f[k]) {
			return bad()
		}
	}
	ids := map[string]uuid.UUID{}
	for _, k := range []string{"tenant_id", "parent_run_id", "instruction_id", "producer_run_id", "producer_task_id"} {
		id, ok := authoredUUID(f[k])
		if !ok {
			return bad()
		}
		ids[k] = id
	}
	for _, k := range []string{"attempt", "fence"} {
		if _, ok := authoredInt(f[k], 1, 1<<53-1); !ok {
			return bad()
		}
	}
	a := authoredEdges{path: path, parent: parent}
	a.add(".recipe", "FrozenRenderRecipe", f["recipe"])
	if _, ok := a.binding(".source", f["source"]); !ok {
		return bad()
	}
	receipts, ok := authoredArray(f["phase_receipts"], int(ordinal-1), int(ordinal-1))
	if !ok {
		return bad()
	}
	seen := map[uuid.UUID]bool{}
	for i, r := range receipts {
		ref, ok := layerRef(r)
		if !ok || seen[ref.ArtifactID] {
			return bad()
		}
		seen[ref.ArtifactID] = true
		a.add(".phase_receipts["+strconv.Itoa(i)+"]", "AuthoredCanonicalPhaseReceipt", r)
	}
	n, e := authoredObject(f["native"], "run_id task_id result_id attempt fence operation operation_version params root image report")
	if e != nil {
		return bad()
	}
	nr, ok := authoredUUID(n["run_id"])
	if !ok || ids["producer_run_id"] == nr || ids["producer_run_id"] == ids["parent_run_id"] {
		return bad()
	}
	for _, k := range []string{"task_id", "result_id"} {
		if _, ok := authoredUUID(n[k]); !ok {
			return bad()
		}
	}
	for _, k := range []string{"attempt", "fence"} {
		if _, ok := authoredInt(n[k], 1, 1<<53-1); !ok {
			return bad()
		}
	}
	op, _ := authoredString(n["operation"])
	ver, _ := authoredString(n["operation_version"])
	root, report := "ImageResult", "ImageReport"
	switch op {
	case "optical.lens", "optical.perspective", "optical.panorama", "optical.focus_stack":
	case "image.upscale":
		root, report = "WorkingImage", "UpscaleReport"
	default:
		return bad()
	}
	if ver != "1" {
		return bad()
	}
	for _, edge := range []struct{ k, c string }{{"params", "Params"}, {"root", root}, {"image", "WorkingImage"}, {"report", report}} {
		a.add(".native."+edge.k, edge.c, n[edge.k])
	}
	target, ok := layerRef(f["target_image"])
	if !ok {
		return bad()
	}
	a.add(".target_image", "WorkingImage", f["target_image"])
	a.add(".layer_mask", "MaskRef", f["layer_mask"])
	// A validation-only WorkingImage applies the established closed canvas and
	// target-chain parser. Its local color/pixels never become emitted edges.
	imageRaw, e := canonical.Marshal(map[string]any{"pixels": json.RawMessage(f["target_image"]), "canvas": json.RawMessage(f["target_canvas"]), "color": map[string]string{"profile_id": "linear_srgb_v1", "icc_sha256": strings.Repeat("a", 64), "encoding": "linear"}, "dtype": "float32", "alpha": nil, "transform_chain": json.RawMessage(f["target_chain"])})
	if e != nil {
		return bad()
	}
	if _, e = review.ParseWorkingImage(imageRaw); e != nil {
		return bad()
	}
	// Reuse strict typed chain traversal; no generic search of nested strings.
	var chain []LayerRef
	if nestedRefs(f["target_chain"], path+".target_chain", "TransformChain", parent, 0, &chain) != nil {
		return bad()
	}
	a.children = append(a.children, chain...)
	var globalCurrent *v2input.Ref
	if !bytes.Equal(f["global_protection"], []byte("null")) {
		pair, e := authoredObject(f["global_protection"], "original current")
		if e != nil {
			return bad()
		}
		o, ok := layerRef(pair["original"])
		c, good := layerRef(pair["current"])
		if !ok || !good || o.ArtifactID == c.ArtifactID {
			return bad()
		}
		globalCurrent = &c
		a.add(".global_protection.original", "MaskRef", pair["original"])
		a.add(".global_protection.current", "MaskRef", pair["current"])
	}
	if !bytes.Equal(f["local_protection"], []byte("null")) {
		r, ok := layerRef(f["local_protection"])
		if !ok || globalCurrent != nil && r.ArtifactID == globalCurrent.ArtifactID {
			return bad()
		}
		a.add(".local_protection", "MaskRef", f["local_protection"])
	}
	masks, ok := authoredArray(f["local_masks"], 1, 256)
	if !ok {
		return bad()
	}
	ordered := []v2input.Ref{}
	seen = map[uuid.UUID]bool{}
	for i, m := range masks {
		w, e := authoredObject(m, "original pixels_sha256 bytes")
		if e != nil || !authoredSHA(w["pixels_sha256"]) {
			return bad()
		}
		if _, ok := authoredInt(w["bytes"], 1, 800004096); !ok {
			return bad()
		}
		r, ok := layerRef(w["original"])
		if !ok || seen[r.ArtifactID] || r.ArtifactID == target.ArtifactID {
			return bad()
		}
		seen[r.ArtifactID] = true
		ordered = append(ordered, r)
		a.add(".local_masks["+strconv.Itoa(i)+"].original", "MaskRef", w["original"])
	}
	occurrences, ok := authoredArray(f["local_occurrences"], 1, 256)
	if !ok {
		return bad()
	}
	unique := []v2input.Ref{}
	seen = map[uuid.UUID]bool{}
	for _, o := range occurrences {
		v, e := authoredObject(o, "path original")
		if e != nil {
			return bad()
		}
		parts, ok := authoredArray(v["path"], 0, 16)
		if !ok {
			return bad()
		}
		for _, p := range parts {
			s, ok := authoredString(p)
			if !ok || (s != "left" && s != "right" && s != "operand") {
				return bad()
			}
		}
		r, ok := layerRef(v["original"])
		if !ok {
			return bad()
		}
		if !seen[r.ArtifactID] {
			seen[r.ArtifactID] = true
			unique = append(unique, r)
		} else {
			for _, x := range unique {
				if x.ArtifactID == r.ArtifactID && x != r {
					return bad()
				}
			}
		}
	}
	if len(unique) != len(ordered) {
		return bad()
	}
	for i := range unique {
		if unique[i] != ordered[i] {
			return bad()
		}
	}
	return a.children, a.err
}
