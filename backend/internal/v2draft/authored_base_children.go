package v2draft

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"unicode/utf8"
)

func authoredBaseRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, e := authoredObject(raw, "schema_version tenant_id parent_run_id base_run_id original_recipe original_source processed_source effective_style phase_plan_sha256 stages quality generated_coverage inherited_review")
	if e != nil {
		return nil, e
	}
	if v, ok := authoredInt(f["schema_version"], 1, 1); !ok || v != 1 {
		return nil, v2input.ErrInvalid
	}
	tenant, ok := authoredUUID(f["tenant_id"])
	p, good := authoredUUID(f["parent_run_id"])
	base, valid := authoredUUID(f["base_run_id"])
	if !ok || !good || !valid || tenant == uuid.Nil || p == base || !authoredSHA(f["phase_plan_sha256"]) {
		return nil, v2input.ErrInvalid
	}
	var review bool
	if json.Unmarshal(f["inherited_review"], &review) != nil || bytes.Equal(f["inherited_review"], []byte("null")) {
		return nil, v2input.ErrInvalid
	}
	a := authoredEdges{path: path, parent: parent}
	a.add(".original_recipe", "FrozenRenderRecipe", f["original_recipe"])
	original, ok := a.binding(".original_source", f["original_source"])
	processed, good := a.binding(".processed_source", f["processed_source"])
	if !ok || !good || original.AssetVersionID != processed.AssetVersionID {
		return nil, v2input.ErrInvalid
	}
	a.add(".effective_style", "StyleV2", f["effective_style"])
	stages, ok := authoredArray(f["stages"], 1, 5000)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	tasks := map[uuid.UUID]bool{}
	results := map[uuid.UUID]bool{}
	for i, rawStage := range stages {
		s, e := authoredObject(rawStage, "ordinal phase operation task_id result_id attempt fence params input_digest execution_profile_sha256 execution_profile_jcs outputs")
		if e != nil {
			return nil, e
		}
		ordinal, ok := authoredInt(s["ordinal"], 0, 4999)
		phase, good := authoredString(s["phase"])
		op, valid := authoredString(s["operation"])
		task, tok := authoredUUID(s["task_id"])
		result, rok := authoredUUID(s["result_id"])
		if !ok || ordinal != int64(i) || !good || len(phase) < 1 || utf8.RuneCountInString(phase) > 80 || phase == "editor_precompose" || !valid || len(op) < 1 || utf8.RuneCountInString(op) > 80 || op == "editor.layers_precompose" || !tok || !rok || tasks[task] || results[result] {
			return nil, v2input.ErrInvalid
		}
		tasks[task] = true
		results[result] = true
		if _, ok = authoredInt(s["attempt"], 1, 1<<53-1); !ok {
			return nil, v2input.ErrInvalid
		}
		if _, ok = authoredInt(s["fence"], 1, 1<<53-1); !ok {
			return nil, v2input.ErrInvalid
		}
		profileText, profileOK := authoredString(s["execution_profile_jcs"])
		if !profileOK || utf8.RuneCountInString(profileText) < 2 || utf8.RuneCountInString(profileText) > 1048576 {
			return nil, v2input.ErrInvalid
		}
		if !authoredSHA(s["input_digest"]) || !authoredProfile(s["execution_profile_jcs"], s["execution_profile_sha256"]) {
			return nil, v2input.ErrInvalid
		}
		prefix := ".stages[" + strconv.Itoa(i) + "]"
		a.add(prefix+".params", "Params", s["params"])
		a.refs(prefix+".outputs", "Artifact", s["outputs"], 1, 128, true)
	}
	a.add(".quality", "QualityReport", f["quality"])
	if !bytes.Equal(f["generated_coverage"], []byte("null")) {
		a.add(".generated_coverage", "MaskRef", f["generated_coverage"])
	}
	return a.children, a.err
}
func authoredContinuationRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, e := authoredObject(raw, "schema_version tenant_id parent_run_id recipe original_source program_sha256 base_receipt stages final_image")
	if e != nil {
		return nil, e
	}
	if _, ok := authoredInt(f["schema_version"], 2, 2); !ok {
		return nil, v2input.ErrInvalid
	}
	for _, key := range []string{"tenant_id", "parent_run_id"} {
		if _, ok := authoredUUID(f[key]); !ok {
			return nil, v2input.ErrInvalid
		}
	}
	if !authoredSHA(f["program_sha256"]) {
		return nil, v2input.ErrInvalid
	}
	a := authoredEdges{path: path, parent: parent}
	a.add(".recipe", "FrozenRenderRecipe", f["recipe"])
	a.binding(".original_source", f["original_source"])
	a.add(".base_receipt", "AuthoredBaseReceipt", f["base_receipt"])
	stages, ok := authoredArray(f["stages"], 1, 32)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	instructions := map[uuid.UUID]bool{}
	phases := map[uuid.UUID]bool{}
	for i, rawStage := range stages {
		s, e := authoredObject(rawStage, "instruction_id phase canonical_receipt input_source")
		if e != nil {
			return nil, e
		}
		instruction, ok := authoredUUID(s["instruction_id"])
		phase, valid := layerRef(s["phase"])
		if !ok || !valid || instructions[instruction] || phases[phase.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		instructions[instruction] = true
		phases[phase.ArtifactID] = true
		prefix := ".stages[" + strconv.Itoa(i) + "]"
		a.add(prefix+".phase", "AuthoredGeometryPhaseResult", s["phase"])
		a.add(prefix+".canonical_receipt", "AuthoredCanonicalPhaseReceipt", s["canonical_receipt"])
		a.binding(prefix+".input_source", s["input_source"])
	}
	a.add(".final_image", "WorkingImage", f["final_image"])
	return a.children, a.err
}
