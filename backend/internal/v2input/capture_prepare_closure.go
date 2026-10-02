package v2input

import "github.com/google/uuid"

type CapturePrepareParams struct {
	SchemaVersion int                   `json:"schema_version"`
	Binding       SessionVersionBinding `json:"binding"`
	Calibration   Ref                   `json:"calibration"`
	ModelLockSHA  string                `json:"model_lock_sha"`
}

// CompileCapturePrepareClosure declares every source child, including inherited
// dense-warp fields that adoption must pin when the proxy keeps their lineage.
func CompileCapturePrepareClosure(workflow uuid.UUID, params CapturePrepareParams, paramsRef Ref,
	source CaptureImageInputs, geometry []Ref) (CaptureClosure, error) {
	if params.SchemaVersion != 1 || params.Binding.AssetVersionID == uuid.Nil ||
		!digestPattern.MatchString(params.Binding.CanvasHash) || !digestPattern.MatchString(params.ModelLockSHA) || len(geometry) > 32 {
		return CaptureClosure{}, ErrInvalid
	}
	task, err := TaskID(workflow, "capture.prepare", 0)
	if err != nil {
		return CaptureClosure{}, err
	}
	entries := []Entry{{paramsRef, "Params"}, {params.Binding.Source, "WorkingImage"},
		{params.Calibration, "Calibration"}, {source.Pixels, "NpyArray"}}
	if source.Alpha != nil {
		entries = append(entries, Entry{*source.Alpha, "NpyArray"})
	}
	for _, ref := range geometry {
		entries = append(entries, Entry{ref, "NpyArray"})
	}
	return compileCaptureInventory(task, "capture.prepare", params, paramsRef, entries)
}
