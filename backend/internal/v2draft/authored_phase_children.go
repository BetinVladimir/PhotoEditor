package v2draft

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

const authoredProducerKeys = "producer_run_id task_id result_id operation operation_version attempt fence params input_digest execution_profile_jcs execution_profile_sha256 outputs"

type authoredProducer struct {
	run, task, result  uuid.UUID
	operation, version string
	outputs            []v2input.Ref
}

func authoredProducerFields(f authoredFields, prefix string, a *authoredEdges) (authoredProducer, error) {
	var out authoredProducer
	var ok bool
	out.run, ok = authoredUUID(f["producer_run_id"])
	if !ok {
		return out, v2input.ErrInvalid
	}
	out.task, ok = authoredUUID(f["task_id"])
	if !ok {
		return out, v2input.ErrInvalid
	}
	out.result, ok = authoredUUID(f["result_id"])
	if !ok {
		return out, v2input.ErrInvalid
	}
	out.operation, ok = authoredString(f["operation"])
	if !ok {
		return out, v2input.ErrInvalid
	}
	out.version, ok = authoredString(f["operation_version"])
	if !ok {
		return out, v2input.ErrInvalid
	}
	for _, key := range []string{"attempt", "fence"} {
		if _, ok := authoredInt(f[key], 1, 1<<53-1); !ok {
			return out, v2input.ErrInvalid
		}
	}
	if !authoredSHA(f["input_digest"]) || !authoredProfile(f["execution_profile_jcs"], f["execution_profile_sha256"]) {
		return out, v2input.ErrInvalid
	}
	a.add(prefix+".params", "Params", f["params"])
	out.outputs = a.refs(prefix+".outputs", "Artifact", f["outputs"], 1, 128, true)
	return out, a.err
}
func authoredContains(refs []v2input.Ref, ref v2input.Ref) bool {
	for _, r := range refs {
		if r == ref {
			return true
		}
	}
	return false
}
func authoredPhaseRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, e := authoredObject(raw, authoredProducerKeys+" schema_version tenant_id parent_run_id instruction_id ordinal kind program_sha256 predecessor_sha256 original_recipe input_source output_source result report physical_closure", "native")
	if e != nil {
		return nil, e
	}
	if _, ok := authoredInt(f["schema_version"], 1, 1); !ok {
		return nil, v2input.ErrInvalid
	}
	if _, ok := authoredInt(f["ordinal"], 1, 4999); !ok {
		return nil, v2input.ErrInvalid
	}
	for _, key := range []string{"tenant_id", "instruction_id"} {
		if _, ok := authoredUUID(f[key]); !ok {
			return nil, v2input.ErrInvalid
		}
	}
	p, ok := authoredUUID(f["parent_run_id"])
	if !ok || !authoredSHA(f["program_sha256"]) || !authoredSHA(f["predecessor_sha256"]) {
		return nil, v2input.ErrInvalid
	}
	a := authoredEdges{path: path, parent: parent}
	producer, e := authoredProducerFields(f, "", &a)
	if e != nil || producer.run == p {
		return nil, v2input.ErrInvalid
	}
	kind, ok := authoredString(f["kind"])
	if !ok {
		return nil, v2input.ErrInvalid
	}
	expected := map[string]string{"segment": "render.authored_segment", "geometry": "render.authored_geometry", "finalize": "render.compose"}
	version := "1"
	if kind == "finalize" {
		version = "2"
	}
	if expected[kind] == "" || producer.operation != expected[kind] || producer.version != version {
		return nil, v2input.ErrInvalid
	}
	a.add(".original_recipe", "FrozenRenderRecipe", f["original_recipe"])
	input, ok := a.binding(".input_source", f["input_source"])
	output, valid := a.binding(".output_source", f["output_source"])
	if !ok || !valid || input.AssetVersionID != output.AssetVersionID {
		return nil, v2input.ErrInvalid
	}
	result, ok := layerRef(f["result"])
	report, valid := layerRef(f["report"])
	if !ok || !valid || !authoredContains(producer.outputs, result) {
		return nil, v2input.ErrInvalid
	}
	resultContract := "ImageResult"
	if kind == "geometry" {
		resultContract = "AuthoredGeometryPhaseResult"
	}
	a.add(".result", resultContract, f["result"])
	a.add(".report", "ImageReport", f["report"])
	physical := a.refs(".physical_closure", "Artifact", f["physical_closure"], 1, 4096, true)
	for _, required := range []v2input.Ref{result, report, output.Source} {
		if !authoredContains(physical, required) {
			return nil, v2input.ErrInvalid
		}
	}
	native, hasNative := f["native"]
	if kind != "geometry" {
		if hasNative || input.CanvasHash != output.CanvasHash {
			return nil, v2input.ErrInvalid
		}
		return a.children, a.err
	}
	if !hasNative {
		return nil, v2input.ErrInvalid
	}
	n, e := authoredObject(native, "task root image report")
	if e != nil {
		return nil, e
	}
	task, e := authoredObject(n["task"], authoredProducerKeys)
	if e != nil {
		return nil, e
	}
	np, e := authoredProducerFields(task, ".native.task", &a)
	if e != nil || np.run == p || np.run == producer.run || np.version != "1" {
		return nil, v2input.ErrInvalid
	}
	switch np.operation {
	case "optical.lens", "optical.perspective", "optical.panorama", "optical.focus_stack", "image.upscale":
	default:
		return nil, v2input.ErrInvalid
	}
	root, ok := layerRef(n["root"])
	image, valid := layerRef(n["image"])
	if !ok || !valid || !authoredContains(np.outputs, root) || root == result || image == output.Source {
		return nil, v2input.ErrInvalid
	}
	nativeContract := "ImageResult"
	if np.operation == "image.upscale" {
		nativeContract = "WorkingImage"
	}
	a.add(".native.root", nativeContract, n["root"])
	a.add(".native.image", "WorkingImage", n["image"])
	a.add(".native.report", "ImageReport", n["report"])
	return a.children, a.err
}
