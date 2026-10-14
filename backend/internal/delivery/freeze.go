// Package delivery compiles immutable delivery inputs. Database authority,
// publication and admission are separate mandatory steps.
package delivery

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type Policy struct {
	RequiredPresets  []uuid.UUID `json:"required_presets"`
	OptionalPresets  []uuid.UUID `json:"optional_presets"`
	RequireReview    bool        `json:"require_review"`
	MetadataPolicy   string      `json:"metadata_policy"`
	ApprovedFields   []string    `json:"approved_fields,omitempty"`
	Provenance       string      `json:"provenance"`
	SigningPolicyID  uuid.UUID   `json:"signing_policy_id,omitempty"`
	SigningPolicySHA string      `json:"signing_policy_sha,omitempty"`
	AllowOmitted     bool        `json:"allow_omitted"`
}
type FrozenPreset struct {
	PresetID  uuid.UUID   `json:"preset_id"`
	PresetSHA string      `json:"preset_sha"`
	Config    v2input.Ref `json:"config"`
	Required  bool        `json:"required"`
}
type FrozenInput struct {
	SchemaVersion  int            `json:"schema_version"`
	Snapshot       v2input.Ref    `json:"snapshot"`
	SnapshotDigest string         `json:"snapshot_digest"`
	Policy         Policy         `json:"policy"`
	Presets        []FrozenPreset `json:"presets"`
}
type PresetContent struct {
	ID  uuid.UUID
	SHA string
	Ref v2input.Ref
	Raw []byte
}

func digest(raw []byte) string { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }

// CompileFrozenInput accepts only scoped, caller-authorized snapshot/config bytes.
// It verifies their physical hashes and existing semantics but does not prove ACL,
// retention, watermark ownership or provenance. No network/database side effects.
func CompileFrozenInput(snapshotRef v2input.Ref, snapshotRaw []byte, policy Policy, configs []PresetContent) (FrozenInput, []byte, string, error) {
	invalid := func() (FrozenInput, []byte, string, error) {
		return FrozenInput{}, nil, "", domain.NewError("VALIDATION_FAILED", 422)
	}
	if snapshotRef.ArtifactID == uuid.Nil || len(snapshotRaw) < 2 || len(snapshotRaw) > 8<<20 || digest(snapshotRaw) != snapshotRef.SHA256 {
		return invalid()
	}
	var snapshot v2input.SessionInputSnapshot
	if json.Unmarshal(snapshotRaw, &snapshot) != nil {
		return invalid()
	}
	_, exact, err := v2input.CompilePinnedSessionInputSnapshot(snapshot.SnapshotID, snapshot.Items)
	if err != nil || !bytes.Equal(snapshotRaw, exact) {
		return invalid()
	}
	if policy.Provenance != "off" && policy.Provenance != "required" {
		return invalid()
	}
	if !validDeliverySigningPolicyChoice(policy) {
		return invalid()
	}
	if policy.MetadataPolicy != "strip" && policy.MetadataPolicy != "copyright" && policy.MetadataPolicy != "approved_exif" {
		return invalid()
	}
	fields, consentErr := normalizeMetadataApproval(policy.MetadataPolicy, policy.ApprovedFields)
	if consentErr != nil {
		return invalid()
	}
	policy.ApprovedFields = fields
	if len(policy.RequiredPresets) < 1 || len(policy.RequiredPresets) > 16 || len(policy.OptionalPresets) > 16 || len(policy.RequiredPresets)+len(policy.OptionalPresets) > 16 {
		return invalid()
	}
	required := make(map[uuid.UUID]bool)
	all := make(map[uuid.UUID]bool)
	for _, id := range policy.RequiredPresets {
		if id == uuid.Nil || all[id] {
			return invalid()
		}
		all[id] = true
		required[id] = true
	}
	for _, id := range policy.OptionalPresets {
		if id == uuid.Nil || all[id] {
			return invalid()
		}
		all[id] = true
	}
	if len(configs) != len(all) {
		return invalid()
	}
	presets := make([]FrozenPreset, 0, len(configs))
	seen := make(map[uuid.UUID]bool)
	for _, item := range configs {
		if !all[item.ID] || seen[item.ID] || item.Ref.ArtifactID == uuid.Nil || item.Ref.SHA256 != item.SHA {
			return invalid()
		}
		config, err := exportpreset.DecodeFrozenConfig(item.Raw, item.SHA)
		if err != nil {
			return invalid()
		}
		// Every preset must carry the same frozen metadata mode. Source approval,
		// physical encoding and adoption are checked by later Delivery stages.
		if config.MetadataPolicy != policy.MetadataPolicy {
			return FrozenInput{}, nil, "", domain.NewError("DELIVERY_METADATA_UNAVAILABLE", 503)
		}
		seen[item.ID] = true
		presets = append(presets, FrozenPreset{item.ID, item.SHA, item.Ref, required[item.ID]})
	}
	sort.Slice(presets, func(i, j int) bool { return presets[i].PresetID.String() < presets[j].PresetID.String() })
	policy.RequiredPresets = append([]uuid.UUID{}, policy.RequiredPresets...)
	policy.OptionalPresets = append([]uuid.UUID{}, policy.OptionalPresets...)
	sort.Slice(policy.RequiredPresets, func(i, j int) bool { return policy.RequiredPresets[i].String() < policy.RequiredPresets[j].String() })
	sort.Slice(policy.OptionalPresets, func(i, j int) bool { return policy.OptionalPresets[i].String() < policy.OptionalPresets[j].String() })
	result := FrozenInput{1, snapshotRef, snapshot.SnapshotDigest, policy, presets}
	raw, err := canonical.Marshal(result)
	if err != nil || len(raw) > 1<<20 {
		return invalid()
	}
	return result, raw, digest(raw), nil
}

func validDeliverySigningPolicyChoice(policy Policy) bool {
	if policy.Provenance == "off" {
		return policy.SigningPolicyID == uuid.Nil && policy.SigningPolicySHA == ""
	}
	return policy.Provenance == "required" && policy.SigningPolicyID != uuid.Nil && shaPattern.MatchString(policy.SigningPolicySHA)
}
