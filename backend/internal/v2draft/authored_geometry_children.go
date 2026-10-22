package v2draft

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

func authoredGeometryRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, e := authoredObject(raw, "schema_version operation operation_version tenant_id parent_run_id instruction_id program_sha256 recipe original_source input_source native blend registered_original blended_result blended_image")
	if e != nil {
		return nil, e
	}
	if _, ok := authoredInt(f["schema_version"], 1, 1); !ok {
		return nil, v2input.ErrInvalid
	}
	op, ok := authoredString(f["operation"])
	ver, good := authoredString(f["operation_version"])
	if !ok || !good || op != "render.authored_geometry" || ver != "1" || !authoredSHA(f["program_sha256"]) {
		return nil, v2input.ErrInvalid
	}
	for _, key := range []string{"tenant_id", "instruction_id"} {
		if _, ok := authoredUUID(f[key]); !ok {
			return nil, v2input.ErrInvalid
		}
	}
	p, ok := authoredUUID(f["parent_run_id"])
	if !ok {
		return nil, v2input.ErrInvalid
	}
	a := authoredEdges{path: path, parent: parent}
	a.add(".recipe", "FrozenRenderRecipe", f["recipe"])
	recipe, _ := layerRef(f["recipe"])
	original, ok := a.binding(".original_source", f["original_source"])
	input, good := a.binding(".input_source", f["input_source"])
	if !ok || !good || original.AssetVersionID != input.AssetVersionID {
		return nil, v2input.ErrInvalid
	}
	native, e := authoredObject(f["native"], "run_id task_id result_id attempt fence operation operation_version params root image report")
	if e != nil {
		return nil, e
	}
	nr, ok := authoredUUID(native["run_id"])
	if !ok || nr == p {
		return nil, v2input.ErrInvalid
	}
	for _, key := range []string{"task_id", "result_id"} {
		if _, ok := authoredUUID(native[key]); !ok {
			return nil, v2input.ErrInvalid
		}
	}
	for _, key := range []string{"attempt", "fence"} {
		if _, ok := authoredInt(native[key], 1, 1<<53-1); !ok {
			return nil, v2input.ErrInvalid
		}
	}
	nativeOp, ok := authoredString(native["operation"])
	nativeVer, good := authoredString(native["operation_version"])
	baselines := map[string]string{"optical.lens": "lens_green_inverse", "optical.perspective": "perspective_source_warp", "optical.panorama": "panorama_reference_registration", "optical.focus_stack": "focus_reference", "image.upscale": "working_linear_resize"}
	if !ok || !good || nativeVer != "1" || baselines[nativeOp] == "" {
		return nil, v2input.ErrInvalid
	}
	rootContract := "ImageResult"
	if nativeOp == "image.upscale" {
		rootContract = "WorkingImage"
	}
	for _, edge := range []struct{ name, contract string }{{"params", "Params"}, {"root", rootContract}, {"image", "WorkingImage"}, {"report", "ImageReport"}} {
		a.add(".native."+edge.name, edge.contract, native[edge.name])
	}
	b, e := authoredObject(f["blend"], "schema_version recipe_sha256 layer_id operation params sources reference_ordinal reference_container_sha256 baseline_kind baseline proposal registration_report target reference_chain color target_mask target_protection opacity blend correspondence")
	if e != nil {
		return nil, e
	}
	if _, ok := authoredInt(b["schema_version"], 1, 1); !ok {
		return nil, v2input.ErrInvalid
	}
	if _, ok := authoredUUID(b["layer_id"]); !ok {
		return nil, v2input.ErrInvalid
	}
	blendOp, _ := authoredString(b["operation"])
	baseline, _ := authoredString(b["baseline_kind"])
	recipeSHA, _ := authoredString(b["recipe_sha256"])
	correspondence, _ := authoredString(b["correspondence"])
	np, _ := layerRef(native["params"])
	bp, bok := layerRef(b["params"])
	ni, _ := layerRef(native["image"])
	proposal, pok := layerRef(b["proposal"])
	if blendOp != nativeOp || baseline != baselines[nativeOp] || recipeSHA != recipe.SHA256 || !bok || !pok || np != bp || ni != proposal || correspondence != "reference_frame_with_retained_source_registrations" || !authoredSHA(b["reference_container_sha256"]) {
		return nil, v2input.ErrInvalid
	}
	var opacity float64
	if json.Unmarshal(b["opacity"], &opacity) != nil || math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity <= 0 || opacity > 1 {
		return nil, v2input.ErrInvalid
	}
	blendMode, _ := authoredString(b["blend"])
	if blendMode != "normal" && blendMode != "multiply" && blendMode != "screen" {
		return nil, v2input.ErrInvalid
	}
	a.add(".blend.params", "Params", b["params"])
	sources, ok := authoredArray(b["sources"], 1, 30)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	multi := nativeOp == "optical.panorama" || nativeOp == "optical.focus_stack"
	if multi != (len(sources) > 1) || nativeOp == "optical.panorama" && len(sources) > 20 {
		return nil, v2input.ErrInvalid
	}
	ordinal, ok := authoredInt(b["reference_ordinal"], 0, int64(len(sources)-1))
	if !ok {
		return nil, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for i, s := range sources {
		binding, valid := a.binding(".blend.sources["+strconv.Itoa(i)+"]", s)
		if !valid || seen[binding.AssetVersionID] || i == int(ordinal) && binding != input {
			return nil, v2input.ErrInvalid
		}
		seen[binding.AssetVersionID] = true
	}
	// Reuse the existing strict canvas/color/chain validator without publishing
	// or granting authority to this local validation-only wrapper.
	imageRaw, e := canonical.Marshal(map[string]any{"pixels": json.RawMessage(b["baseline"]), "canvas": json.RawMessage(b["target"]), "color": json.RawMessage(b["color"]), "dtype": "float32", "alpha": nil, "transform_chain": json.RawMessage(b["reference_chain"])})
	if e != nil {
		return nil, v2input.ErrInvalid
	}
	image, e := review.ParseWorkingImage(imageRaw)
	containerSHA, _ := authoredString(b["reference_container_sha256"])
	if e != nil || image.Canvas.SourceSHA256 != containerSHA {
		return nil, v2input.ErrInvalid
	}
	for _, edge := range []struct{ name, contract string }{{"baseline", "WorkingImage"}, {"proposal", "WorkingImage"}, {"registration_report", "ImageReport"}} {
		a.add(".blend."+edge.name, edge.contract, b[edge.name])
	}
	// Validated transform-chain fields carry only their explicit inverse fields.
	var chainChildren []LayerRef
	if e = nestedRefs(b["reference_chain"], path+".blend.reference_chain", "TransformChain", parent, 0, &chainChildren); e != nil {
		return nil, e
	}
	for _, child := range chainChildren {
		if child.Contract != "DenseWarpField" {
			return nil, v2input.ErrInvalid
		}
	}
	a.children = append(a.children, chainChildren...)
	a.add(".blend.target_mask", "MaskRef", b["target_mask"])
	if !bytes.Equal(b["target_protection"], []byte("null")) {
		a.add(".blend.target_protection", "MaskRef", b["target_protection"])
	}
	refs := map[uuid.UUID]bool{}
	var result, imageRef v2input.Ref
	for _, edge := range []struct{ name, contract string }{{"registered_original", "WorkingImage"}, {"blended_result", "ImageResult"}, {"blended_image", "WorkingImage"}} {
		ref, valid := layerRef(f[edge.name])
		if !valid || refs[ref.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		refs[ref.ArtifactID] = true
		a.add("."+edge.name, edge.contract, f[edge.name])
		if edge.name == "blended_result" {
			result = ref
		}
		if edge.name == "blended_image" {
			imageRef = ref
		}
	}
	nrRef, _ := layerRef(native["root"])
	if result == nrRef || imageRef == ni {
		return nil, v2input.ErrInvalid
	}
	return a.children, a.err
}
