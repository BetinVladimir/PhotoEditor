package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"sort"
	"strings"
	"unicode/utf8"
)

type SlotShard struct {
	SchemaVersion  int    `json:"schema_version"`
	SnapshotDigest string `json:"snapshot_digest"`
	Items          []Slot `json:"items"`
}
type AssemblyInput struct {
	SchemaVersion  int            `json:"schema_version"`
	SnapshotDigest string         `json:"snapshot_digest"`
	ItemCount      int            `json:"item_count"`
	SlotCount      int            `json:"slot_count"`
	Policy         Policy         `json:"policy"`
	Presets        []FrozenPreset `json:"presets"`
	Shards         []v2input.Ref  `json:"shards"`
}

// PublishDocument must use durable scope+purpose idempotency, current tenant/run
// authority, reservation and versioned object adoption. It must not infer IDs.
type PublishDocument func(context.Context, string, []byte) (v2input.Ref, error)

// BuildAssemblyInput validates the entire matrix before publishing any shard.
// The frozen input/configs must already be authorized, retained and byte-verified
// by admission. Publication outside a DB transaction may leave GC-able orphans
// on failure; the coordinator must attach the complete result atomically.
func BuildAssemblyInput(ctx context.Context, frozen FrozenInput, snapshotRaw []byte, inputSlots []Slot, publish PublishDocument) (AssemblyInput, []byte, string, error) {
	return buildAssemblyInput(ctx, frozen, snapshotRaw, inputSlots, publish, false)
}

func buildAssemblyInput(ctx context.Context, frozen FrozenInput, snapshotRaw []byte, inputSlots []Slot, publish PublishDocument, signed bool) (AssemblyInput, []byte, string, error) {
	invalid := func() (AssemblyInput, []byte, string, error) {
		return AssemblyInput{}, nil, "", domain.NewError("DELIVERY_MATRIX_INVALID", 409)
	}
	if err := ctx.Err(); err != nil {
		return AssemblyInput{}, nil, "", err
	}
	if publish == nil || frozen.SchemaVersion != 1 || frozen.Snapshot.ArtifactID == uuid.Nil || len(snapshotRaw) < 2 || len(snapshotRaw) > 8<<20 || digest(snapshotRaw) != frozen.Snapshot.SHA256 || !shaPattern.MatchString(frozen.SnapshotDigest) {
		return invalid()
	}
	// Own all nested policy/preset fields before publication callbacks can yield.
	raw, err := canonical.Marshal(frozen)
	if err != nil || json.Unmarshal(raw, &frozen) != nil {
		return invalid()
	}
	if frozen.Policy.Provenance == "required" && !signed {
		return AssemblyInput{}, nil, "", domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
	}
	if !validDeliverySigningPolicyChoice(frozen.Policy) ||
		(signed && frozen.Policy.Provenance != "required") ||
		(!signed && frozen.Policy.Provenance != "off") ||
		len(frozen.Policy.RequiredPresets) < 1 || len(frozen.Policy.RequiredPresets) > 16 || len(frozen.Policy.OptionalPresets) > 16 || len(frozen.Policy.RequiredPresets)+len(frozen.Policy.OptionalPresets) > 16 || len(frozen.Presets) < 1 || len(frozen.Presets) > 16 {
		return invalid()
	}
	if frozen.Policy.MetadataPolicy != "strip" && frozen.Policy.MetadataPolicy != "copyright" && frozen.Policy.MetadataPolicy != "approved_exif" {
		return invalid()
	}
	approved, approvalErr := normalizeMetadataApproval(frozen.Policy.MetadataPolicy, frozen.Policy.ApprovedFields)
	if approvalErr != nil || len(approved) != len(frozen.Policy.ApprovedFields) {
		return invalid()
	}
	for index, field := range approved {
		if frozen.Policy.ApprovedFields[index] != field {
			return invalid()
		}
	}
	frozen.Policy.ApprovedFields = approved
	required := map[uuid.UUID]bool{}
	ids := map[uuid.UUID]bool{}
	for _, id := range frozen.Policy.RequiredPresets {
		if id == uuid.Nil || ids[id] {
			return invalid()
		}
		ids[id] = true
		required[id] = true
	}
	for _, id := range frozen.Policy.OptionalPresets {
		if id == uuid.Nil || ids[id] {
			return invalid()
		}
		ids[id] = true
	}
	presets := map[uuid.UUID]FrozenPreset{}
	for _, p := range frozen.Presets {
		if !ids[p.PresetID] || presets[p.PresetID].PresetID != uuid.Nil || p.Required != required[p.PresetID] || p.Config.ArtifactID == uuid.Nil || !shaPattern.MatchString(p.PresetSHA) || p.PresetSHA != p.Config.SHA256 {
			return invalid()
		}
		presets[p.PresetID] = p
	}
	if len(presets) != len(ids) {
		return invalid()
	}
	sort.Slice(frozen.Presets, func(i, j int) bool { return frozen.Presets[i].PresetID.String() < frozen.Presets[j].PresetID.String() })
	var snapshot v2input.SessionInputSnapshot
	if json.Unmarshal(snapshotRaw, &snapshot) != nil {
		return invalid()
	}
	_, exact, err := v2input.CompilePinnedSessionInputSnapshot(snapshot.SnapshotID, snapshot.Items)
	if err != nil || !bytes.Equal(exact, snapshotRaw) || snapshot.SnapshotDigest != frozen.SnapshotDigest || len(inputSlots) != len(snapshot.Items)*len(presets) {
		return invalid()
	}
	slots := make([]Slot, len(inputSlots))
	seenExports := map[uuid.UUID]bool{}
	for i, slot := range inputSlots {
		slots[i] = slot
		if slot.ExportID != nil {
			id := *slot.ExportID
			slots[i].ExportID = &id
		}
		if slot.Export != nil {
			ref := *slot.Export
			slots[i].Export = &ref
		}
		if slot.Reason != nil {
			reason := *slot.Reason
			slots[i].Reason = &reason
		}
	}
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].Ordinal != slots[j].Ordinal {
			return slots[i].Ordinal < slots[j].Ordinal
		}
		return slots[i].PresetID.String() < slots[j].PresetID.String()
	})
	for i, slot := range slots {
		if err := ctx.Err(); err != nil {
			return AssemblyInput{}, nil, "", err
		}
		ordinal, presetIndex := i/len(presets), i%len(presets)
		p := frozen.Presets[presetIndex]
		if slot.Ordinal != ordinal || slot.PresetID != p.PresetID || slot.PresetSHA != p.PresetSHA || slot.Required != p.Required || slot.Binding != snapshot.Items[ordinal].Binding {
			return invalid()
		}
		switch slot.State {
		case "ready":
			if slot.ExportID == nil || *slot.ExportID == uuid.Nil || slot.Export == nil || slot.Export.ArtifactID == uuid.Nil || !shaPattern.MatchString(slot.Export.SHA256) || slot.Reason != nil {
				return invalid()
			}
			if seenExports[*slot.ExportID] {
				return invalid()
			}
			seenExports[*slot.ExportID] = true
		case "omitted":
			if slot.Required || !frozen.Policy.AllowOmitted || slot.ExportID != nil || slot.Export != nil || slot.Reason == nil || !utf8.ValidString(*slot.Reason) || strings.TrimSpace(*slot.Reason) == "" || utf8.RuneCountInString(*slot.Reason) > 200 {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	refs := make([]v2input.Ref, 0, (len(slots)+499)/500)
	seenRefs := map[uuid.UUID]bool{}
	for offset := 0; offset < len(slots); offset += 500 {
		if err := ctx.Err(); err != nil {
			return AssemblyInput{}, nil, "", err
		}
		end := offset + 500
		if end > len(slots) {
			end = len(slots)
		}
		body, err := canonical.Marshal(SlotShard{1, frozen.SnapshotDigest, slots[offset:end]})
		if err != nil || len(body) > 1<<20 {
			return invalid()
		}
		hash := digest(body)
		ref, err := publish(ctx, fmt.Sprintf("v30_delivery_slots_%03d", offset/500), append([]byte(nil), body...))
		if err != nil {
			return AssemblyInput{}, nil, "", err
		}
		if err := ctx.Err(); err != nil {
			return AssemblyInput{}, nil, "", err
		}
		if ref.ArtifactID == uuid.Nil || seenRefs[ref.ArtifactID] || ref.SHA256 != hash {
			return invalid()
		}
		seenRefs[ref.ArtifactID] = true
		refs = append(refs, ref)
	}
	if err := ctx.Err(); err != nil {
		return AssemblyInput{}, nil, "", err
	}
	result := AssemblyInput{1, frozen.SnapshotDigest, len(snapshot.Items), len(slots), frozen.Policy, frozen.Presets, refs}
	body, err := canonical.Marshal(result)
	if err != nil || len(body) > 1<<20 {
		return invalid()
	}
	return result, body, digest(body), nil
}
