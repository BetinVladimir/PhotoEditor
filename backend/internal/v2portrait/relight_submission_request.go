package v2portrait

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

const MaxRelightSubmissionBytes = 131072

type relightSubmissionWire struct {
	SchemaVersion int             `json:"schema_version"`
	Snapshot      uuid.UUID       `json:"snapshot_id"`
	Run           uuid.UUID       `json:"run_id"`
	Workflow      uuid.UUID       `json:"workflow_id"`
	Ordinal       int             `json:"ordinal"`
	SubjectKind   string          `json:"subject_kind"`
	Params        json.RawMessage `json:"params"`
}

// DecodeRelightSubmissionRequest is a service boundary, not an HTTP route.
// Identity, tenant, qualification, runtime and capacity fields are forbidden.
func DecodeRelightSubmissionRequest(raw []byte) (RelightSubmissionRequest, error) {
	var zero RelightSubmissionRequest
	if len(raw) < 2 || len(raw) > MaxRelightSubmissionBytes {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var w relightSubmissionWire
	if json.Unmarshal(normal, &w) != nil || w.SchemaVersion != 1 || w.Snapshot == uuid.Nil || w.Run == uuid.Nil || w.Workflow == uuid.Nil || w.Ordinal < 0 || w.Ordinal >= 5000 || (w.SubjectKind != "manual" && w.SubjectKind != "person") || len(w.Params) > 65536 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(w)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	var p RelightParams
	if json.Unmarshal(w.Params, &p) != nil {
		return zero, v2input.ErrInvalid
	}
	compiled, _, err := p.Compile()
	if err != nil || !bytes.Equal(compiled, w.Params) {
		return zero, v2input.ErrInvalid
	}
	return RelightSubmissionRequest{Snapshot: w.Snapshot, Run: w.Run, Workflow: w.Workflow, Ordinal: w.Ordinal, SubjectKind: w.SubjectKind, ParamsRaw: append([]byte(nil), compiled...)}, nil
}
