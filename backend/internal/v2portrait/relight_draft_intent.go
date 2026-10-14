package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type relightDraftIntentWire struct {
	SchemaVersion int             `json:"schema_version"`
	Snapshot      uuid.UUID       `json:"snapshot_id"`
	Ordinal       int             `json:"ordinal"`
	SubjectKind   string          `json:"subject_kind"`
	Params        json.RawMessage `json:"params"`
}

// DecodeRelightDraftIntent accepts authored controls only, without run IDs or
// deployment authority. Required fields and canonical UUIDs are exact.
func DecodeRelightDraftIntent(raw []byte) (RelightDraftIntent, error) {
	var zero RelightDraftIntent
	if len(raw) < 2 || len(raw) > MaxRelightSubmissionBytes {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var w relightDraftIntentWire
	if json.Unmarshal(normal, &w) != nil || w.SchemaVersion != 1 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(w)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	// Reuse the full strict Params validator; these temporary IDs grant no authority.
	run := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	workflow, _ := v2input.WorkflowID(run)
	body, err := canonical.Marshal(relightSubmissionWire{SchemaVersion: 1, Snapshot: w.Snapshot, Run: run, Workflow: workflow, Ordinal: w.Ordinal, SubjectKind: w.SubjectKind, Params: w.Params})
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	request, err := DecodeRelightSubmissionRequest(body)
	if err != nil {
		return zero, err
	}
	return RelightDraftIntent{Snapshot: request.Snapshot, Ordinal: request.Ordinal, SubjectKind: request.SubjectKind, ParamsRaw: request.ParamsRaw}, nil
}

// SubmitProjectKeyed checks router scope before the bound service can touch SQL.
func (s *RelightSubmissionService) SubmitProjectKeyed(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, key string, intent RelightDraftIntent) ([]byte, error) {
	if s == nil || tenant != s.tenant || project != s.project {
		return nil, v2input.ErrInvalid
	}
	return s.SubmitKeyed(ctx, who, key, intent)
}
