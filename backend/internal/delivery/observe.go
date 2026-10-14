package delivery

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"regexp"
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var reasonPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,79}$`)

type SlotExpectation struct {
	SnapshotDigest string
	Ordinal        int
	Binding        v2input.SessionVersionBinding
	Preset         FrozenPreset
	ExportID       uuid.UUID
}

// ExportObservation must be supplied by a scoped, locked repository; these
// values alone are not proof of ACL, object bytes or final provenance.
type ExportObservation struct {
	ExportID       uuid.UUID                     `json:"export_id"`
	SnapshotDigest string                        `json:"snapshot_digest"`
	Binding        v2input.SessionVersionBinding `json:"binding"`
	PresetSHA      string                        `json:"preset_sha"`
	RunState       string                        `json:"run_state"`
	ExportState    string                        `json:"export_state"`
	ArtifactReady  bool                          `json:"artifact_ready"`
	Artifact       *v2input.Ref                  `json:"artifact"`
	Reason         string                        `json:"reason"`
}
type Slot struct {
	Ordinal   int                           `json:"ordinal"`
	Binding   v2input.SessionVersionBinding `json:"binding"`
	PresetID  uuid.UUID                     `json:"preset_id"`
	PresetSHA string                        `json:"preset_sha"`
	Required  bool                          `json:"required"`
	State     string                        `json:"state"`
	ExportID  *uuid.UUID                    `json:"export_id"`
	Export    *v2input.Ref                  `json:"export"`
	Reason    *string                       `json:"reason"`
}

// ObserveExport never mutates child runs or substitutes a newer preset/binding.
// Pending is an internal state and must not be serialized into assembly shards.
func ObserveExport(expected SlotExpectation, observed ExportObservation, policy Policy) (Slot, error) {
	invalid := func() (Slot, error) { return Slot{}, domain.NewError("DELIVERY_EXPORT_MISMATCH", 409) }
	if expected.Ordinal < 0 || expected.Ordinal > 4999 || expected.ExportID == uuid.Nil || expected.Preset.PresetID == uuid.Nil || expected.Binding.AssetVersionID == uuid.Nil || expected.Binding.Source.ArtifactID == uuid.Nil || !shaPattern.MatchString(expected.Binding.Source.SHA256) || !shaPattern.MatchString(expected.Binding.CanvasHash) || !shaPattern.MatchString(expected.SnapshotDigest) || !shaPattern.MatchString(expected.Preset.PresetSHA) {
		return invalid()
	}
	if observed.ExportID != expected.ExportID || observed.SnapshotDigest != expected.SnapshotDigest || observed.Binding != expected.Binding || observed.PresetSHA != expected.Preset.PresetSHA {
		return invalid()
	}
	if len(policy.RequiredPresets) < 1 || len(policy.RequiredPresets) > 16 || len(policy.OptionalPresets) > 16 || len(policy.RequiredPresets)+len(policy.OptionalPresets) > 16 {
		return invalid()
	}
	seen := map[uuid.UUID]bool{}
	matched := false
	for _, id := range policy.RequiredPresets {
		if id == uuid.Nil || seen[id] {
			return invalid()
		}
		seen[id] = true
		if id == expected.Preset.PresetID {
			if !expected.Preset.Required {
				return invalid()
			}
			matched = true
		}
	}
	for _, id := range policy.OptionalPresets {
		if id == uuid.Nil || seen[id] {
			return invalid()
		}
		seen[id] = true
		if id == expected.Preset.PresetID {
			if expected.Preset.Required {
				return invalid()
			}
			matched = true
		}
	}
	if !matched {
		return invalid()
	}
	// Export observation describes only the unsigned encoded child. Required
	// signing is a separate dependency and receives no success credit here.
	if policy.Provenance != "off" && policy.Provenance != "required" {
		return invalid()
	}
	slot := Slot{Ordinal: expected.Ordinal, Binding: expected.Binding, PresetID: expected.Preset.PresetID, PresetSHA: expected.Preset.PresetSHA, Required: expected.Preset.Required, State: "pending"}
	switch observed.RunState {
	case "queued", "running", "waiting_review", "cancelling":
		return slot, nil
	case "completed", "completed_with_errors", "failed", "cancelled":
	default:
		return invalid()
	}
	if observed.RunState == "completed" && observed.ExportState == "ready" && observed.ArtifactReady && observed.Artifact != nil && observed.Artifact.ArtifactID != uuid.Nil && shaPattern.MatchString(observed.Artifact.SHA256) {
		id := observed.ExportID
		artifact := *observed.Artifact
		slot.State = "ready"
		slot.ExportID = &id
		slot.Export = &artifact
		return slot, nil
	}
	reason := "EXPORT_FAILED"
	if observed.RunState == "cancelled" {
		reason = "EXPORT_CANCELLED"
	} else if reasonPattern.MatchString(observed.Reason) {
		reason = observed.Reason
	} else if observed.RunState == "completed" {
		reason = "EXPORT_NOT_READY"
	}
	slot.State = "failed"
	if !slot.Required && policy.AllowOmitted {
		slot.State = "omitted"
	}
	slot.Reason = &reason
	return slot, nil
}

// MatrixState enforces all-terminal assembly; required/forbidden omissions fail
// the package even if other children are still running. Inventory completeness
// and exact pairs must be checked against the frozen snapshot by the coordinator.
func MatrixState(slots []Slot) (string, error) {
	if len(slots) == 0 || len(slots) > 80000 {
		return "", domain.NewError("VALIDATION_FAILED", 422)
	}
	pending := false
	failed := false
	for _, slot := range slots {
		switch slot.State {
		case "pending":
			pending = true
		case "failed":
			failed = true
		case "omitted":
			if slot.Required {
				failed = true
			}
		case "ready":
		default:
			return "", domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	if failed {
		return "failed", nil
	}
	if pending {
		return "pending", nil
	}
	return "assemble", nil
}
