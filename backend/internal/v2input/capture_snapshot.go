package v2input

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type CaptureSnapshot struct {
	SchemaVersion int                   `json:"schema_version"`
	SnapshotID    uuid.UUID             `json:"snapshot_id"`
	ReceiptID     uuid.UUID             `json:"receipt_id"`
	SessionID     uuid.UUID             `json:"session_id"`
	Binding       SessionVersionBinding `json:"binding"`
}

func CaptureDraftIDs(job uuid.UUID) (run, workflow, snapshot uuid.UUID, err error) {
	if job == uuid.Nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, ErrInvalid
	}
	run = uuid.NewSHA1(job, []byte("capture.run"))
	workflow, err = WorkflowID(run)
	snapshot = uuid.NewSHA1(run, []byte("capture.snapshot"))
	return
}
func CompileCaptureSnapshot(l CaptureAssessmentLease, binding SessionVersionBinding) (CaptureSnapshot, json.RawMessage, string, error) {
	_, _, id, err := CaptureDraftIDs(l.JobID)
	if err != nil || !validCaptureLease(l) || binding.AssetVersionID != l.AssetVersionID || !validRef(binding.Source) || !digestPattern.MatchString(binding.CanvasHash) {
		return CaptureSnapshot{}, nil, "", ErrInvalid
	}
	value := CaptureSnapshot{1, id, l.ReceiptID, l.SessionID, binding}
	body, err := canonical.Marshal(value)
	if err != nil {
		return CaptureSnapshot{}, nil, "", err
	}
	hash, err := canonical.Hash(value)
	return value, body, hash, err
}
