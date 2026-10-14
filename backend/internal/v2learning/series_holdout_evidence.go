package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// FrozenSeriesHoldoutEvidence is a value document, never source authority.
// SQL must prove the producer, item and source lineage before publication and
// again before Fit/Evaluate task admission.
type FrozenSeriesHoldoutEvidence struct {
	SchemaVersion  int         `json:"schema_version"`
	TenantID       uuid.UUID   `json:"tenant_id"`
	EventID        uuid.UUID   `json:"event_id"`
	AssetVersionID uuid.UUID   `json:"asset_version_id"`
	ApplyRunID     uuid.UUID   `json:"apply_run_id"`
	ItemID         uuid.UUID   `json:"item_id"`
	ProducerTaskID uuid.UUID   `json:"producer_task_id"`
	SourceImage    v2input.Ref `json:"source_image"`
	StageImage     v2input.Ref `json:"stage_image"`
	Params         v2input.Ref `json:"params"`
	Series         v2input.Ref `json:"series"`
}

func validFrozenSeriesHoldout(value FrozenSeriesHoldoutEvidence) bool {
	if value.SchemaVersion != 1 || value.TenantID == uuid.Nil ||
		value.EventID == uuid.Nil || value.AssetVersionID == uuid.Nil ||
		value.ApplyRunID == uuid.Nil || value.ItemID == uuid.Nil ||
		value.ProducerTaskID == uuid.Nil {
		return false
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range []v2input.Ref{value.SourceImage, value.StageImage,
		value.Params, value.Series} {
		if !validManifestRef(ref) || seen[ref.ArtifactID] {
			return false
		}
		seen[ref.ArtifactID] = true
	}
	return true
}

func CompileFrozenSeriesHoldoutEvidence(value FrozenSeriesHoldoutEvidence) ([]byte, string, error) {
	if !validFrozenSeriesHoldout(value) {
		return nil, "", ErrInvalidTrainingDraft
	}
	body, err := canonical.Marshal(value)
	if err != nil || len(body) > 16384 {
		return nil, "", ErrInvalidTrainingDraft
	}
	digest := sha256.Sum256(body)
	return body, hex.EncodeToString(digest[:]), nil
}

func DecodeFrozenSeriesHoldoutEvidence(raw []byte, ref v2input.Ref,
	expected FrozenSeriesHoldoutEvidence) (FrozenSeriesHoldoutEvidence, error) {
	if len(raw) < 2 || len(raw) > 16384 || !validManifestRef(ref) ||
		!validFrozenSeriesHoldout(expected) {
		return FrozenSeriesHoldoutEvidence{}, ErrInvalidTrainingDraft
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return FrozenSeriesHoldoutEvidence{}, ErrInvalidTrainingDraft
	}
	canonicalRaw, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalRaw, raw) {
		return FrozenSeriesHoldoutEvidence{}, ErrInvalidTrainingDraft
	}
	var value FrozenSeriesHoldoutEvidence
	if json.Unmarshal(raw, &value) != nil || !validFrozenSeriesHoldout(value) ||
		value != expected {
		return FrozenSeriesHoldoutEvidence{}, ErrInvalidTrainingDraft
	}
	return value, nil
}

// runID is the training run, not the source Apply run. This identity is stable
// across an ambiguous publication result.
func SeriesHoldoutRef(runID, eventID uuid.UUID, sha string) (v2input.Ref, error) {
	if runID == uuid.Nil || eventID == uuid.Nil || !validSHA(sha) {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	return v2input.Ref{ArtifactID: uuid.NewSHA1(runID,
		[]byte("v23-series/"+eventID.String())), SHA256: sha}, nil
}
