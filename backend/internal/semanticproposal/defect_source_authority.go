package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type defectSourceAuthority struct {
	SchemaVersion int                           `json:"schema_version"`
	Binding       v2input.SessionVersionBinding `json:"binding"`
	SourcePins    json.RawMessage               `json:"source_pins"`
	Expected      json.RawMessage               `json:"expected"`
	Qualification json.RawMessage               `json:"qualification"`
	Policy        json.RawMessage               `json:"policy"`
}

// Owns source and defect qualification evidence selected from one full policy
// snapshot. Face/parser/manual/person authority remains a separate requirement.
// This packet does not itself authorize catalog reads or graph activation.
func (s SQLTransferSourceReader) marshalDefectSourceAuthority(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectPolicyExecutionPreparation) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validQualificationPolicySnapshot(s, who, tenant, prepared.policy) {
		return nil, v2input.ErrInvalid
	}
	if err := matchDefectExecutionPolicySnapshot(prepared.execution, prepared.policy); err != nil {
		return nil, err
	}
	source := prepared.execution.face.source
	if _, err := s.decodeDefectExecutionFilePins(ctx, who, tenant, binding, source); err != nil {
		return nil, err
	}
	pins, err := marshalSourceEvidencePins(source.source)
	if err != nil {
		return nil, err
	}
	var expected v2qualification.Expected
	if json.Unmarshal([]byte(source.expected), &expected) != nil {
		return nil, v2input.ErrInvalid
	}
	qualification, err := s.marshalDefectQualificationPreparation(ctx, who, tenant, source.qualification, expected)
	if err != nil {
		return nil, err
	}
	body, err := canonical.Marshal(defectSourceAuthority{SchemaVersion: 1, Binding: binding, SourcePins: pins, Expected: json.RawMessage(source.expected), Qualification: qualification, Policy: json.RawMessage(prepared.policy.body)})
	if err != nil {
		return nil, err
	}
	if len(body) > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}
