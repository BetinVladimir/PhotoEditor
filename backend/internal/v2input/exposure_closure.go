package v2input

import (
	"encoding/json"
	"math"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ExposureDependencies contains immutable children resolved from verified
// WorkingImage/MaskRef documents. Resolving their bytes and live authority is
// required before admission; inventory compilation does not establish either.
type ExposureDependencies struct {
	Reference                                Ref
	Source, ReferenceImage                   CaptureImageInputs
	MaskPixels, Protection, ProtectionPixels Ref
	Geometry                                 []Ref
}

type exposureParams struct {
	SchemaVersion   int                   `json:"schema_version"`
	TargetID        uuid.UUID             `json:"target_id"`
	Source          SessionVersionBinding `json:"source"`
	Intent          Ref                   `json:"intent"`
	Candidate       Ref                   `json:"candidate"`
	Mask            Ref                   `json:"mask"`
	Transform       Ref                   `json:"transform"`
	Kind            string                `json:"kind"`
	Strength        float64               `json:"strength"`
	EV              *float64              `json:"ev"`
	Prompt          *string               `json:"prompt"`
	PreservePersons bool                  `json:"preserve_persons"`
}

// CompileExposureClosure freezes the complete readable inventory for the
// approved CPU edit. It uses the review child/target UUID namespace, rather
// than the workflow ordinal namespace used by transfer proposals. Unsupported
// kinds cannot be routed through this adapter.
func CompileExposureClosure(child uuid.UUID, raw json.RawMessage, paramsRef Ref, dependencies ExposureDependencies) (uuid.UUID, CaptureClosure, error) {
	invalid := func() (uuid.UUID, CaptureClosure, error) { return uuid.Nil, CaptureClosure{}, ErrInvalid }
	if child == uuid.Nil || len(raw) < 2 || len(raw) > 1048576 || len(dependencies.Geometry) > 64 {
		return invalid()
	}
	fields, ok := semanticObject(raw, map[string]bool{"prompt": true}, "schema_version", "target_id", "source", "intent", "candidate", "mask", "transform", "kind", "strength", "ev", "prompt", "preserve_persons")
	if !ok || !semanticBindingJSON(fields["source"]) {
		return invalid()
	}
	for _, name := range []string{"intent", "candidate", "mask", "transform"} {
		if !semanticRefJSON(fields[name]) {
			return invalid()
		}
	}
	var params exposureParams
	if !semanticDecode(raw, &params) || params.SchemaVersion != 1 || params.TargetID == uuid.Nil ||
		params.Source.AssetVersionID == uuid.Nil || !digestPattern.MatchString(params.Source.CanvasHash) ||
		params.Kind != "local_exposure" || !params.PreservePersons || math.IsNaN(params.Strength) || math.IsInf(params.Strength, 0) ||
		params.Strength < 0 || params.Strength > 1 || params.EV == nil || math.IsNaN(*params.EV) || math.IsInf(*params.EV, 0) || *params.EV < -1 || *params.EV > 1 {
		return invalid()
	}
	if params.Prompt != nil && (!utf8.ValidString(*params.Prompt) || utf8.RuneCountInString(*params.Prompt) < 1 || utf8.RuneCountInString(*params.Prompt) > 1024) {
		return invalid()
	}
	task := uuid.NewSHA1(child, []byte("semantic.edit:"+params.TargetID.String()))
	if paramsRef.ArtifactID != uuid.NewSHA1(task, []byte("Params")) {
		return invalid()
	}
	entries := []Entry{{paramsRef, "Params"}, {params.Source.Source, "WorkingImage"},
		{params.Intent, "EditIntent"}, {params.Candidate, "TransferCandidate"}, {params.Mask, "MaskRef"},
		{params.Transform, "ResolvedTransform"}, {dependencies.Reference, "WorkingImage"},
		{dependencies.Source.Pixels, "NpyArray"}, {dependencies.ReferenceImage.Pixels, "NpyArray"},
		{dependencies.MaskPixels, "NpyArray"}, {dependencies.Protection, "MaskRef"}, {dependencies.ProtectionPixels, "NpyArray"}}
	for _, alpha := range []*Ref{dependencies.Source.Alpha, dependencies.ReferenceImage.Alpha} {
		if alpha != nil {
			entries = append(entries, Entry{*alpha, "NpyArray"})
		}
	}
	for _, field := range dependencies.Geometry {
		entries = append(entries, Entry{field, "NpyArray"})
	}
	// Keep the original raw document in the hash comparison. Re-encoding a
	// permissively decoded struct must never hide an unknown or omitted field.
	closure, err := compileCaptureInventory(task, "semantic.local_exposure", json.RawMessage(append([]byte(nil), raw...)), paramsRef, entries)
	if err != nil {
		return invalid()
	}
	return task, closure, nil
}
