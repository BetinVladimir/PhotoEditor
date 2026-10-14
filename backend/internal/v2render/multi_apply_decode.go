package v2render

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func DecodeMultiApplyDecision(raw []byte) (MultiApplyDecision, error) {
	var value MultiApplyDecision
	if len(raw) < 2 || len(raw) > 65536 {
		return value, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || json.Unmarshal(normalized, &value) != nil {
		return MultiApplyDecision{}, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(value)
	if err != nil || !bytes.Equal(exact, normalized) || value.SchemaVersion != 1 || value.ResultID == uuid.Nil || value.Root.ArtifactID == uuid.Nil || value.QualityRun == uuid.Nil || value.QualityResult == uuid.Nil || value.QualityReport.ArtifactID == uuid.Nil || !validSHA256(value.Root.SHA256) || !validSHA256(value.QualityReport.SHA256) || !value.ConfirmGeometryReset {
		return MultiApplyDecision{}, v2input.ErrInvalid
	}
	return value, nil
}
