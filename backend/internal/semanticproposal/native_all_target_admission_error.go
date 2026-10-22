package semanticproposal

import "fmt"

func nativeAllTargetAdmissionError(stage string, err error) error {
	if err == nil {
		return nil
	}
	switch stage {
	case "saved_parent_replay", "activate_parent", "replay_sealed_parent", "seal_parent", "admission_receipt", "begin_draft", "compile_plan", "completed_after", "completed_before", "completed_evidence", "context", "defect_seal", "detector_link", "detector_seal", "input_documents", "persist_parent", "person_policy", "physical_after", "physical_before", "plan_scope", "publish_params", "publish_workflow", "recover_defect_transport", "reference_documents", "reference_person", "register_inputs", "runtime_configuration", "saved_parent_lookup", "scene_evidence", "scene_policy", "source_person", "source_proof", "source_protection", "targets":
	default:
		stage = "unknown"
	}
	return fmt.Errorf("native all-target admission %s: %w", stage, err)
}
