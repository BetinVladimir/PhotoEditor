package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"photoeditor/backend/internal/canonical"
	"reflect"
)

// GPU memory is a conservative allocator bound, never a sampled peak.
type OperatorMetalMemory struct {
	Metric    string `json:"metric"`
	Ceiling   int64  `json:"allocator_ceiling_bytes"`
	SampleMax int64  `json:"driver_sample_max_bytes"`
	Samples   int64  `json:"samples"`
}
type OperatorModelInvocation struct {
	Model   string  `json:"model_id"`
	Backend string  `json:"backend"`
	Method  string  `json:"method"`
	Elapsed float64 `json:"elapsed_seconds"`
}
type OperatorMeasuredResources struct {
	GPU                *OperatorMetalMemory `json:"gpu_memory,omitempty"`
	Scope              string               `json:"scope"`
	PeakRSS            int64                `json:"peak_rss_bytes"`
	PeakVRAM           int64                `json:"peak_vram_bytes"`
	ScratchReserved    int64                `json:"scratch_reserved_peak_bytes"`
	ScratchObserved    int64                `json:"scratch_observed_peak_bytes"`
	ScratchMeasurement string               `json:"scratch_measurement"`
}
type operatorObservation struct {
	ExecutionBackend string                      `json:"execution_backend,omitempty"`
	ModelInvocations []OperatorModelInvocation   `json:"model_invocations,omitempty"`
	Resources        OperatorMeasuredResources   `json:"resources"`
	Schema           int                         `json:"schema_version"`
	Boundary         string                      `json:"boundary"`
	Task             PreparedTask                `json:"task"`
	ElapsedSeconds   float64                     `json:"elapsed_seconds"`
	ReviewRequired   bool                        `json:"review_required"`
	Outputs          []operatorObservationObject `json:"outputs"`
}
type operatorObservationObject struct {
	ArtifactID string  `json:"artifact_id"`
	SHA256     string  `json:"sha256"`
	Bytes      int64   `json:"bytes"`
	MediaType  string  `json:"media_type"`
	Role       string  `json:"role"`
	Contract   *string `json:"contract"`
	Path       string  `json:"path"`
}

func validateOperatorObservation(task PreparedTask, measured OperatorMeasuredTask) error {
	raw, e := canonical.Bytes(measured.ObservationRaw)
	if e != nil || !bytes.Equal(raw, measured.ObservationRaw) {
		return errors.New("operator observation must be canonical")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != measured.ObservationSHA256 {
		return errors.New("operator observation digest mismatch")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var doc operatorObservation
	if e = decoder.Decode(&doc); e != nil {
		return e
	}
	if (doc.Schema != 1 && doc.Schema != 2) || doc.Boundary != "operator_lower_compute_no_sql_receipts" || !reflect.DeepEqual(doc.Task, task) || len(doc.Outputs) == 0 || len(doc.Outputs) != len(measured.Outputs) {
		return errors.New("operator observation scope invalid")
	}
	if e := validateOperatorResourceEvidence(task, doc, raw); e != nil {
		return e
	}
	total := int64(0)
	for i, proof := range measured.Outputs {
		item := doc.Outputs[i]
		a := proof.Artifact
		if item.ArtifactID != a.ArtifactID.String() || item.SHA256 != a.SHA256 || item.Bytes != a.Bytes || item.MediaType != a.MediaType || item.Role != proof.Role || proof.Verifier == nil {
			return errors.New("operator observation output mismatch")
		}
		expected, e := proof.Verifier.path(a.Key)
		if e != nil || expected != item.Path {
			return errors.New("operator observation output path mismatch")
		}
		if item.Contract == nil && proof.Contract != "" || item.Contract != nil && *item.Contract != proof.Contract {
			return errors.New("operator observation contract mismatch")
		}
		total += a.Bytes
	}
	if total != measured.OutputBytes {
		return errors.New("operator observation byte count mismatch")
	}
	return nil
}

func validateOperatorResourceEvidence(task PreparedTask, doc operatorObservation, raw []byte) error {
	resources := doc.Resources
	if resources.PeakRSS < 1 || resources.PeakVRAM != 0 || resources.ScratchReserved < 0 || resources.ScratchObserved < 0 || resources.ScratchObserved > resources.ScratchReserved || resources.ScratchMeasurement != "reservation_upper_bound_and_transition_samples" {
		return errors.New("operator observed resources invalid")
	}

	var profile struct {
		Backend string `json:"backend"`
	}
	if json.Unmarshal(task.ExecutionProfile, &profile) != nil {
		return errors.New("operator execution backend invalid")
	}
	if doc.Schema == 1 {
		if profile.Backend != "cpu" || resources.Scope != "whole_current_cpu_process_highwater" || resources.GPU != nil || doc.ExecutionBackend != "" || len(doc.ModelInvocations) != 0 {
			return errors.New("operator CPU observation invalid")
		}
	} else {
		var envelope map[string]json.RawMessage
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &envelope) != nil || json.Unmarshal(envelope["resources"], &fields) != nil {
			return errors.New("operator resource object invalid")
		}
		_, forgedPeak := fields["peak_vram_bytes"]
		gpu := resources.GPU
		if forgedPeak || profile.Backend != "mps" || doc.ExecutionBackend != "mps" || resources.Scope != "whole_current_mixed_process_highwater" || gpu == nil || gpu.Metric != "pytorch_allocator_conservative_ceiling" || gpu.Ceiling < 1 || gpu.Ceiling > 9007199254740991 || gpu.SampleMax < 0 || gpu.SampleMax > gpu.Ceiling || gpu.Samples < 2 || len(doc.ModelInvocations) == 0 {
			return errors.New("operator Metal observation invalid")
		}
		for _, call := range doc.ModelInvocations {
			if call.Model == "" || call.Backend != "mps" || (call.Method != "infer" && call.Method != "generate") || math.IsNaN(call.Elapsed) || math.IsInf(call.Elapsed, 0) || call.Elapsed <= 0 || call.Elapsed > 10800 {
				return errors.New("operator Metal inference proof invalid")
			}
		}
	}

	return nil
}
