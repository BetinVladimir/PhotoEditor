package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// PortraitDetailHoldoutOrigin is an identity supplied by a future current SQL
// producer reader. Possessing this value does not grant publication authority.
type PortraitDetailHoldoutOrigin struct {
	TenantID, EventID, AssetVersionID           uuid.UUID
	RunID, TaskID, ResultID, AttemptID          uuid.UUID
	Source, Image, Protection, Params, Resolved v2input.Ref
	MaskArtifacts                               []v2input.Ref
}

type FrozenPortraitDetailHoldoutEvidence struct {
	SchemaVersion  int           `json:"schema_version"`
	TenantID       uuid.UUID     `json:"tenant_id"`
	EventID        uuid.UUID     `json:"event_id"`
	AssetVersionID uuid.UUID     `json:"asset_version_id"`
	RunID          uuid.UUID     `json:"run_id"`
	TaskID         uuid.UUID     `json:"task_id"`
	ResultID       uuid.UUID     `json:"result_id"`
	AttemptID      uuid.UUID     `json:"attempt_id"`
	Source         v2input.Ref   `json:"source"`
	Image          v2input.Ref   `json:"image"`
	Protection     v2input.Ref   `json:"protection"`
	Params         v2input.Ref   `json:"params"`
	Resolved       v2input.Ref   `json:"resolved"`
	MaskArtifacts  []v2input.Ref `json:"mask_artifacts"`
}

func validPortraitDetailHoldoutEvidence(value FrozenPortraitDetailHoldoutEvidence) bool {
	if value.SchemaVersion != 1 || value.TenantID == uuid.Nil ||
		value.EventID == uuid.Nil || value.AssetVersionID == uuid.Nil ||
		value.RunID == uuid.Nil || value.TaskID == uuid.Nil ||
		value.ResultID == uuid.Nil || value.AttemptID == uuid.Nil ||
		len(value.MaskArtifacts) < 1 || len(value.MaskArtifacts) > 160 {
		return false
	}
	refs := make([]v2input.Ref, 0, 5+len(value.MaskArtifacts))
	refs = append(refs, value.Source, value.Image, value.Protection,
		value.Params, value.Resolved)
	refs = append(refs, value.MaskArtifacts...)
	seen := make(map[uuid.UUID]bool, len(refs))
	for _, ref := range refs {
		if !validManifestRef(ref) || seen[ref.ArtifactID] {
			return false
		}
		seen[ref.ArtifactID] = true
	}
	for index := 1; index < len(value.MaskArtifacts); index++ {
		left, right := value.MaskArtifacts[index-1].ArtifactID, value.MaskArtifacts[index].ArtifactID
		if bytes.Compare(left[:], right[:]) >= 0 {
			return false
		}
	}
	return true
}

// CompileFrozenPortraitDetailHoldoutEvidence produces exact original bytes;
// caller must still prove SQL event/producer/source/mask authority and publish
// only through the tenant-scoped immutable artifact protocol.
func CompileFrozenPortraitDetailHoldoutEvidence(origin PortraitDetailHoldoutOrigin,
	maskArtifacts []v2input.Ref) (FrozenPortraitDetailHoldoutEvidence, []byte, string, error) {
	invalid := func() (FrozenPortraitDetailHoldoutEvidence, []byte, string, error) {
		return FrozenPortraitDetailHoldoutEvidence{}, nil, "", ErrInvalidTrainingDraft
	}
	frozen := FrozenPortraitDetailHoldoutEvidence{
		SchemaVersion: 1, TenantID: origin.TenantID, EventID: origin.EventID,
		AssetVersionID: origin.AssetVersionID, RunID: origin.RunID,
		TaskID: origin.TaskID, ResultID: origin.ResultID,
		AttemptID: origin.AttemptID, Source: origin.Source, Image: origin.Image,
		Protection: origin.Protection, Params: origin.Params,
		Resolved: origin.Resolved, MaskArtifacts: append([]v2input.Ref(nil), maskArtifacts...),
	}
	if !validPortraitDetailHoldoutEvidence(frozen) ||
		!slices.Equal(frozen.MaskArtifacts, origin.MaskArtifacts) {
		return invalid()
	}
	body, err := canonical.Marshal(frozen)
	if err != nil || len(body) > 1048576 {
		return invalid()
	}
	digest := sha256.Sum256(body)
	return frozen, body, hex.EncodeToString(digest[:]), nil
}

// DecodeFrozenPortraitDetailHoldoutEvidence refuses any producer substitution
// even when the caller supplies another internally consistent document hash.
func DecodeFrozenPortraitDetailHoldoutEvidence(body []byte, ref v2input.Ref,
	expected PortraitDetailHoldoutOrigin) (FrozenPortraitDetailHoldoutEvidence, error) {
	invalid := func() (FrozenPortraitDetailHoldoutEvidence, error) {
		return FrozenPortraitDetailHoldoutEvidence{}, ErrInvalidTrainingDraft
	}
	if len(body) < 2 || len(body) > 1048576 || !validManifestRef(ref) {
		return invalid()
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return invalid()
	}
	var frozen FrozenPortraitDetailHoldoutEvidence
	if json.Unmarshal(body, &frozen) != nil || !validPortraitDetailHoldoutEvidence(frozen) {
		return invalid()
	}
	canonicalBody, err := canonical.Marshal(frozen)
	if err != nil || !bytes.Equal(canonicalBody, body) ||
		frozen.TenantID != expected.TenantID || frozen.EventID != expected.EventID ||
		frozen.AssetVersionID != expected.AssetVersionID ||
		frozen.RunID != expected.RunID || frozen.TaskID != expected.TaskID ||
		frozen.ResultID != expected.ResultID || frozen.AttemptID != expected.AttemptID ||
		frozen.Source != expected.Source || frozen.Image != expected.Image ||
		frozen.Protection != expected.Protection ||
		frozen.Params != expected.Params || frozen.Resolved != expected.Resolved ||
		!slices.Equal(frozen.MaskArtifacts, expected.MaskArtifacts) {
		return invalid()
	}
	return frozen, nil
}
