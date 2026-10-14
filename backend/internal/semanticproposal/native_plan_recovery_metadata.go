package semanticproposal

import (
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

// Saved compiler inputs are locators, never qualification/admission authority.
// Capacity limits use the exact original compiled estimates: recompilation must
// reproduce that profile and the original graph config before recovery proceeds.
type nativePlanRecoveryMetadata struct {
	SkinProfileVersion   int                            `json:"skin_profile_version,omitempty"`
	FaceProfileVersion   int                            `json:"face_profile_version,omitempty"`
	PersonProfileVersion int                            `json:"person_profile_version,omitempty"`
	OrdinaryPolicy       *createProjectPolicy           `json:"ordinary_policy,omitempty"`
	Continuation         string                         `json:"continuation,omitempty"`
	RequestKey           string                         `json:"request_key,omitempty"`
	Schema               int                            `json:"schema_version"`
	Report               *v2input.Ref                   `json:"qualification_report"`
	Costs                v2input.SemanticQualifiedCosts `json:"qualified_costs"`
	RAM                  int64                          `json:"ram_limit_bytes"`
	VRAM                 int64                          `json:"vram_limit_bytes"`
	Scratch              int64                          `json:"scratch_limit_bytes"`
	Output               int64                          `json:"output_limit_bytes"`
	Reserved             int64                          `json:"reserved_bytes"`
}

func nativePlanRecoveryFromTask(task v2input.DraftTask, report *v2input.Ref, costs v2input.SemanticQualifiedCosts, reserved int64) (nativePlanRecoveryMetadata, error) {
	var zero nativePlanRecoveryMetadata
	var profile struct {
		Backend  string `json:"backend"`
		Resource struct {
			RAM     int64 `json:"ram_bytes"`
			VRAM    int64 `json:"vram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
			Output  int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if len(task.ExecutionProfile) > 1<<20 || json.Unmarshal(task.ExecutionProfile, &profile) != nil {
		return zero, v2input.ErrInvalid
	}
	expectedBackend := "cuda"
	switch task.Operation {
	case "face.detect", "face.landmarks", "face.parse", "mask.person":
	case "defects.propose":
		expectedBackend = "cpu"
	case "mask.face_skin":
		expectedBackend = "cpu"
		if report != nil || costs != (v2input.SemanticQualifiedCosts{}) {
			return zero, v2input.ErrInvalid
		}
	default:
		return zero, v2input.ErrInvalid
	}
	if task.Operation == "mask.person" && costs.VRAMBytes <= 0 {
		return zero, v2input.ErrInvalid
	}
	if profile.Backend != expectedBackend || (task.Operation != "mask.face_skin" && report == nil) {
		return zero, v2input.ErrInvalid
	}
	if report != nil {
		if report.ArtifactID == uuid.Nil {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(report.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	p := profile.Resource
	for _, value := range []int64{p.RAM, p.VRAM, p.Scratch, p.Output, reserved, costs.RAMBytes, costs.VRAMBytes, costs.ScratchBytes} {
		if value < 0 || value > 9007199254740991 {
			return zero, v2input.ErrInvalid
		}
	}
	if p.RAM == 0 || p.Output == 0 || p.Output > 9007199254740991-(8<<20) || reserved < p.Output+(8<<20) || costs.RAMBytes > p.RAM || costs.VRAMBytes > p.VRAM || costs.ScratchBytes > p.Scratch {
		return zero, v2input.ErrInvalid
	}
	var owned *v2input.Ref
	if report != nil {
		copy := *report
		owned = &copy
	}
	version := 0
	if task.Operation == "mask.person" || task.Operation == "face.detect" || task.Operation == "face.landmarks" || task.Operation == "face.parse" {
		var identities map[string]json.RawMessage
		if json.Unmarshal(task.ExecutionProfile, &identities) != nil {
			return zero, v2input.ErrInvalid
		}
		count := 0
		values := map[string]string{}
		for _, key := range []string{"model_lock_id", "model_lock_sha", "runtime_lock_sha", "device_profile_sha"} {
			if raw, ok := identities[key]; ok {
				count++
				var value string
				if json.Unmarshal(raw, &value) != nil {
					return zero, v2input.ErrInvalid
				}
				if _, err := domain.ParseDigest(value); err != nil {
					return zero, v2input.ErrInvalid
				}
				values[key] = value
			}
		}
		if count != 0 {
			if count != 4 || values["model_lock_id"] != values["model_lock_sha"] {
				return zero, v2input.ErrInvalid
			}
			version = 1
		}
	}
	skinVersion := 0
	var fields map[string]json.RawMessage
	if json.Unmarshal(task.ExecutionProfile, &fields) != nil {
		return zero, v2input.ErrInvalid
	}
	if raw, present := fields["input_index_scratch_bytes"]; present {
		var size int64
		if task.Operation != "mask.face_skin" || json.Unmarshal(raw, &size) != nil || size != 16<<20 || p.Scratch < size {
			return zero, v2input.ErrInvalid
		}
		skinVersion = 1
	}
	personVersion, faceVersion := 0, 0
	if task.Operation == "mask.person" {
		personVersion = version
	} else {
		faceVersion = version
	}
	return nativePlanRecoveryMetadata{SkinProfileVersion: skinVersion, FaceProfileVersion: faceVersion, PersonProfileVersion: personVersion, Schema: 1, Report: owned, Costs: costs, RAM: p.RAM, VRAM: p.VRAM, Scratch: p.Scratch, Output: p.Output, Reserved: reserved}, nil
}

func decodeNativePlanRecoveryMetadata(raw []byte, task v2input.DraftTask) (nativePlanRecoveryMetadata, error) {
	var zero, value nativePlanRecoveryMetadata
	if len(raw) < 2 || len(raw) > 1<<20 || json.Unmarshal(raw, &value) != nil || value.Schema != 1 {
		return zero, v2input.ErrInvalid
	}
	compiled, err := nativePlanRecoveryFromTask(task, value.Report, value.Costs, value.Reserved)
	if err != nil {
		return zero, err
	}
	if value.RequestKey != "" {
		if (task.Operation != "face.detect" && task.Operation != "mask.person" && task.Operation != "face.landmarks" && task.Operation != "face.parse") || !idempotency.ValidKey(value.RequestKey) {
			return zero, v2input.ErrInvalid
		}
		compiled.RequestKey = value.RequestKey
	}
	if value.Continuation != "" {
		if task.Operation != "mask.person" || value.Continuation != "ordinary_parent" {
			return zero, v2input.ErrInvalid
		}
		if value.OrdinaryPolicy == nil || value.OrdinaryPolicy.ProjectID == uuid.Nil {
			return zero, v2input.ErrInvalid
		}
		compiled.OrdinaryPolicy = value.OrdinaryPolicy
		compiled.Continuation = value.Continuation
	}
	if value.Continuation == "" && value.OrdinaryPolicy != nil {
		return zero, v2input.ErrInvalid
	}
	actual, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		return zero, err
	}
	expected, err := canonical.Hash(compiled)
	if err != nil || expected != actual {
		return zero, v2input.ErrConflict
	}
	return compiled, nil
}
