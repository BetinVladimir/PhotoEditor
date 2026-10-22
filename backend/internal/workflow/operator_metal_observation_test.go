package workflow

import (
	"encoding/json"
	"testing"
)

func metalDoc() (PreparedTask, operatorObservation, []byte) {
	task := PreparedTask{ExecutionProfile: json.RawMessage(`{"backend":"mps"}`)}
	doc := operatorObservation{Schema: 2, ExecutionBackend: "mps", ModelInvocations: []OperatorModelInvocation{{Model: "real_model", Backend: "mps", Method: "infer", Elapsed: .1}}, Resources: OperatorMeasuredResources{Scope: "whole_current_mixed_process_highwater", PeakRSS: 100, ScratchReserved: 20, ScratchObserved: 10, ScratchMeasurement: "reservation_upper_bound_and_transition_samples", GPU: &OperatorMetalMemory{Metric: "pytorch_allocator_conservative_ceiling", Ceiling: 20000000000, SampleMax: 500, Samples: 3}}}
	raw, _ := json.Marshal(doc)
	// Schema2 deliberately omits the legacy peak field; it is never a zero peak.
	var object map[string]any
	_ = json.Unmarshal(raw, &object)
	delete(object["resources"].(map[string]any), "peak_vram_bytes")
	raw, _ = json.Marshal(object)
	return task, doc, raw
}
func TestOperatorMetalResourceEvidence(t *testing.T) {
	task, doc, raw := metalDoc()
	if err := validateOperatorResourceEvidence(task, doc, raw); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"backend", "missing_gpu", "sample_bound", "zero_calls", "cpu_call", "nan_time", "forged_peak"} {
		t.Run(kind, func(t *testing.T) {
			task, doc, raw := metalDoc()
			switch kind {
			case "backend":
				task.ExecutionProfile = json.RawMessage(`{"backend":"cpu"}`)
			case "missing_gpu":
				doc.Resources.GPU = nil
			case "sample_bound":
				doc.Resources.GPU.SampleMax = doc.Resources.GPU.Ceiling + 1
			case "zero_calls":
				doc.ModelInvocations = nil
			case "cpu_call":
				doc.ModelInvocations[0].Backend = "cpu"
			case "nan_time":
				doc.ModelInvocations[0].Elapsed = 0
			case "forged_peak":
				raw = []byte(`{"resources":{"peak_vram_bytes":500}}`)
			}
			if validateOperatorResourceEvidence(task, doc, raw) == nil {
				t.Fatal("invalid Metal evidence accepted")
			}
		})
	}
}
func TestOperatorCPUResourceEvidenceUnchanged(t *testing.T) {
	task := PreparedTask{ExecutionProfile: json.RawMessage(`{"backend":"cpu"}`)}
	doc := operatorObservation{Schema: 1, Resources: OperatorMeasuredResources{Scope: "whole_current_cpu_process_highwater", PeakRSS: 100, ScratchReserved: 20, ScratchObserved: 10, ScratchMeasurement: "reservation_upper_bound_and_transition_samples"}}
	if err := validateOperatorResourceEvidence(task, doc, nil); err != nil {
		t.Fatal(err)
	}
	doc.Resources.GPU = &OperatorMetalMemory{}
	if validateOperatorResourceEvidence(task, doc, nil) == nil {
		t.Fatal("GPU evidence relabelled CPU")
	}
}
