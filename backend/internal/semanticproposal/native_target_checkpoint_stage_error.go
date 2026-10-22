package semanticproposal

import "fmt"

// Closed labels expose no task IDs, object keys, Params or credentials.
// This changes diagnostics only; original predicates and causes remain exact.
var nativeTargetCheckpointStepLabels = [...]string{"advance_target_persons", "advance_target_detectors", "advance_target_faces", "advance_target_skins", "advance_target_defects", "advance_scene_prerequisites"}

func nativeTargetCheckpointStageError(stage string, err error) error {
	if err == nil {
		return nil
	}
	switch stage {
	case "restore_manifest", "restore_face_producers", "read_manual_presence", "manual_selection_present", "read_defect_params", "missing_person_locator", "decode_defect_params", "restore_defect_plan", "restore_registered_protection", "advance_target_persons", "advance_target_detectors", "advance_target_faces", "advance_target_skins", "advance_target_defects", "advance_scene_prerequisites", "admit_parent", "parent_ticket_identity":
		return fmt.Errorf("native target checkpoint %s: %w", stage, err)
	default:
		return err
	}
}
