package v2draft

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

func authoredGeometryTaskParamRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, bool, error) {
	var header map[string]json.RawMessage
	if json.Unmarshal(raw, &header) != nil {
		return nil, false, nil
	}
	// Distinct pre-output geometry marker. A partial/malformed marker cannot
	// fall through to opaque generic Params traversal.
	_, marker := header["parallel_source"]
	if !marker {
		return nil, false, nil
	}
	children, e := authoredGeometryTaskRefs(raw, path, parent)
	return children, true, e
}
func authoredGeometryTaskRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	bad := func() ([]LayerRef, error) { return nil, v2input.ErrInvalid }
	f, e := authoredObject(raw, "schema_version tenant_id parent_run_id workflow_id instruction_id ordinal program_sha256 predecessor_sha256 recipe base_receipt source parallel_source authored_params native phase_receipts")
	if e != nil {
		return bad()
	}
	if _, ok := authoredInt(f["schema_version"], 1, 1); !ok {
		return bad()
	}
	ordinal, ok := authoredInt(f["ordinal"], 1, 4998)
	if !ok {
		return bad()
	}
	ids := map[string]uuid.UUID{}
	for _, k := range []string{"tenant_id", "parent_run_id", "workflow_id", "instruction_id"} {
		id, ok := authoredUUID(f[k])
		if !ok {
			return bad()
		}
		ids[k] = id
	}
	if ids["instruction_id"] != uuid.NewSHA1(ids["workflow_id"], []byte("render.authored_geometry:"+strconv.FormatInt(ordinal, 10))) || !authoredSHA(f["program_sha256"]) || !authoredSHA(f["predecessor_sha256"]) {
		return bad()
	}
	a := authoredEdges{path: path, parent: parent}
	a.add(".recipe", "FrozenRenderRecipe", f["recipe"])
	a.add(".base_receipt", "AuthoredBaseReceipt", f["base_receipt"])
	a.add(".parallel_source", "WorkingImage", f["parallel_source"])
	a.add(".authored_params", "Params", f["authored_params"])
	if _, ok := a.binding(".source", f["source"]); !ok {
		return bad()
	}
	n, e := authoredObject(f["native"], "task root image report")
	if e != nil {
		return bad()
	}
	task, e := authoredObject(n["task"], authoredProducerKeys)
	if e != nil {
		return bad()
	}
	producer, e := authoredProducerFields(task, ".native.task", &a)
	if e != nil || producer.version != "1" || producer.run == ids["parent_run_id"] || producer.run == uuid.NewSHA1(ids["instruction_id"], []byte("AuthoredGeometryRun/v1")) {
		return bad()
	}
	rootContract, reportContract := "ImageResult", "ImageReport"
	switch producer.operation {
	case "optical.lens", "optical.perspective", "optical.panorama", "optical.focus_stack":
	case "image.upscale":
		rootContract, reportContract = "WorkingImage", "UpscaleReport"
	default:
		return bad()
	}
	root, ok := layerRef(n["root"])
	image, good := layerRef(n["image"])
	if !ok || !good || !authoredContains(producer.outputs, root) || producer.operation == "image.upscale" && root != image {
		return bad()
	}
	a.add(".native.root", rootContract, n["root"])
	a.add(".native.image", "WorkingImage", n["image"])
	a.add(".native.report", reportContract, n["report"])
	refs := a.refs(".phase_receipts", "AuthoredCanonicalPhaseReceipt", f["phase_receipts"], int(ordinal-1), int(ordinal-1), true)
	recipe, ok := layerRef(f["recipe"])
	base, good := layerRef(f["base_receipt"])
	if !ok || !good || recipe.ArtifactID == base.ArtifactID {
		return bad()
	}
	for _, r := range refs {
		if r.ArtifactID == recipe.ArtifactID || r.ArtifactID == base.ArtifactID {
			return bad()
		}
	}
	// Same-ID SHA or typed-contract conflicts are never resolved by edge order.
	hashes := map[uuid.UUID]string{}
	contracts := map[uuid.UUID]string{}
	for _, c := range a.children {
		if h, ok := hashes[c.Ref.ArtifactID]; ok && h != c.Ref.SHA256 {
			return bad()
		}
		hashes[c.Ref.ArtifactID] = c.Ref.SHA256
		if c.Contract == "Artifact" {
			continue
		}
		if k, ok := contracts[c.Ref.ArtifactID]; ok && k != c.Contract {
			return bad()
		}
		contracts[c.Ref.ArtifactID] = c.Contract
	}
	return a.children, a.err
}
