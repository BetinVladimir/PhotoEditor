package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type CapturePolicy struct {
	SchemaVersion      int      `json:"schema_version"`
	Checks             []string `json:"checks"`
	MaxQueueAgeSeconds int      `json:"max_queue_age_seconds"`
	Calibration        Ref      `json:"calibration"`
}
type CaptureAssessParams struct {
	SchemaVersion int                   `json:"schema_version"`
	Binding       SessionVersionBinding `json:"binding"`
	Proxy         Ref                   `json:"proxy"`
	Faces         Ref                   `json:"faces"`
	Policy        CapturePolicy         `json:"policy"`
	ModelLockSHA  string                `json:"model_lock_sha"`
	Context       Ref                   `json:"context"`
}
type CaptureImageInputs struct {
	Pixels Ref
	Alpha  *Ref
}
type CaptureClosure struct {
	Open        CursorFactory
	Direct      []Ref
	InputDigest string
	ParamsJCS   json.RawMessage
}

// CompileCaptureClosure freezes all readable documents and pixel children.
// Its cursor never consults a mutable session or receipt during a retry.
func CompileCaptureClosure(taskID uuid.UUID, params CaptureAssessParams, paramsRef Ref,
	source, proxy CaptureImageInputs, shots, consents Ref) (CaptureClosure, error) {
	if taskID == uuid.Nil || params.SchemaVersion != 1 || params.Binding.AssetVersionID == uuid.Nil ||
		!digestPattern.MatchString(params.Binding.CanvasHash) || !digestPattern.MatchString(params.ModelLockSHA) ||
		params.Policy.SchemaVersion != 1 || params.Policy.MaxQueueAgeSeconds != 30 || len(params.Policy.Checks) < 1 || len(params.Policy.Checks) > 6 {
		return CaptureClosure{}, ErrInvalid
	}
	allowed := map[string]bool{"focus": true, "eyes": true, "clipping": true, "glare": true, "duplicate": true, "shot_list": true}
	for _, check := range params.Policy.Checks {
		if !allowed[check] {
			return CaptureClosure{}, ErrInvalid
		}
		allowed[check] = false
	}
	entries := []Entry{{paramsRef, "Params"}, {params.Binding.Source, "WorkingImage"}, {params.Proxy, "WorkingImage"},
		{params.Faces, "FaceObservations"}, {params.Policy.Calibration, "Calibration"}, {params.Context, "CaptureContext"},
		{shots, "ShotList"}, {consents, "ConsentSnapshot"}, {source.Pixels, "NpyArray"}, {proxy.Pixels, "NpyArray"}}
	for _, alpha := range []*Ref{source.Alpha, proxy.Alpha} {
		if alpha != nil {
			entries = append(entries, Entry{*alpha, "NpyArray"})
		}
	}
	return compileCaptureInventory(taskID, "capture.assess", params, paramsRef, entries)
}

func compileCaptureInventory(taskID uuid.UUID, operation string, params any, paramsRef Ref, entries []Entry) (CaptureClosure, error) {

	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	unique := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if !validEntry(entry) {
			return CaptureClosure{}, ErrInvalid
		}
		if len(unique) > 0 && unique[len(unique)-1].Ref.ArtifactID == entry.Ref.ArtifactID {
			if unique[len(unique)-1] != entry {
				return CaptureClosure{}, ErrInvalid
			}
			continue
		}
		unique = append(unique, entry)
	}
	body, err := canonical.Marshal(params)
	if err != nil {
		return CaptureClosure{}, err
	}
	hash, err := canonical.Hash(params)
	if err != nil || hash != paramsRef.SHA256 {
		return CaptureClosure{}, ErrInvalid
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID `json:"task"`
		Operation string    `json:"operation"`
		Version   string    `json:"version"`
		Entries   []Entry   `json:"entries"`
	}{taskID, operation, "1", unique})
	if err != nil {
		return CaptureClosure{}, err
	}
	refs := make([]Ref, len(unique))
	for i, entry := range unique {
		refs[i] = entry.Ref
	}
	return CaptureClosure{Open: func(context.Context) (Cursor, error) { return &sessionEntryCursor{entries: unique}, nil },
		Direct: refs, InputDigest: digest, ParamsJCS: body}, nil
}
