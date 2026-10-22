package v2draft

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Typed traversal exposes only a real report Ref. Calibration measurements and
// evidence SHA strings are not artifact references or proof of qualification.
func authoredFinalGenerationRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, e := authoredObject(raw, "schema_version operation operation_version contracts_sha authored_kernel_version qualification qualification_report calibration_sha256 calibration")
	if e != nil {
		return nil, e
	}
	if _, ok := authoredInt(f["schema_version"], 1, 1); !ok {
		return nil, v2input.ErrInvalid
	}
	for key, want := range map[string]string{"operation": "render.compose", "operation_version": "2", "contracts_sha": "338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b", "authored_kernel_version": "v2.authored.1"} {
		v, ok := authoredString(f[key])
		if !ok || v != want {
			return nil, v2input.ErrInvalid
		}
	}
	q, e := authoredObject(f["qualification"], "model_lock_sha runtime_lock_sha backend device_profile_sha dataset_sha")
	if e != nil {
		return nil, e
	}
	for _, key := range []string{"model_lock_sha", "runtime_lock_sha", "device_profile_sha", "dataset_sha"} {
		if !authoredSHA(q[key]) {
			return nil, v2input.ErrInvalid
		}
	}
	b, ok := authoredString(q["backend"])
	if !ok || b != "cpu" || !authoredSHA(f["calibration_sha256"]) {
		return nil, v2input.ErrInvalid
	}
	var calibration map[string]json.RawMessage
	if json.Unmarshal(f["calibration"], &calibration) != nil || calibration == nil {
		return nil, v2input.ErrInvalid
	}
	var hidden []LayerRef
	if nestedRefs(f["calibration"], path+".calibration", "Artifact", parent, 0, &hidden) != nil || len(hidden) != 0 {
		return nil, v2input.ErrInvalid
	}
	ref, ok := layerRef(f["qualification_report"])
	if !ok || ref.ArtifactID == parent.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	return []LayerRef{{Path: path + ".qualification_report", Contract: "QualificationReport", Ref: ref, ParentRef: parent}}, nil
}
func authoredQualificationReportLeaf(raw []byte) error {
	// Structural reader only. Verify against trusted Expected remains mandatory
	// in the physical generation reader; this parser never issues a passed grant.
	var r v2qualification.Report
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&r) != nil {
		return v2input.ErrInvalid
	}
	round, e := canonical.Marshal(r)
	if e != nil || !bytes.Equal(round, raw) || r.SchemaVersion != 1 {
		return v2input.ErrInvalid
	}
	var refs []LayerRef
	if nestedRefs(raw, "Report", "Artifact", v2input.Ref{}, 0, &refs) != nil || len(refs) != 0 {
		return v2input.ErrInvalid
	}
	return nil
}
