package semanticproposal

import "fmt"

// Closed stage labels carry no task IDs, Params, object locators or credentials.
// Every wrapped original remains available through errors.Is/errors.As.
func nativeInitialRecoveryStageError(stage string, err error) error {
	if err == nil {
		return nil
	}
	switch stage {
	case "validate", "all_native_ticket_identity", "detector_ids", "prepared_ticket_identity", "initial_ticket_identity", "manual_presence_invalid", "read_terminal", "load_anchor", "resolve_catalog", "read_all_native_parent", "resume_all_native_parent", "restore_all_native_producers", "read_target_checkpoint", "recover_target_checkpoint", "ensure_initial_person", "advance_face_pipeline", "read_person_state", "classify_person_state", "restore_person_plan", "read_person_input", "read_manual_presence", "advance_initial_defect", "save_target_checkpoint", "read_manual_selection", "read_manual_terminal", "resume_manual_defect", "advance_target_persons", "advance_target_detectors", "advance_target_faces", "advance_target_skins", "advance_target_defects", "advance_scene_prerequisites", "admit_prepared_parent", "admit_initial_parent":
		return fmt.Errorf("native initial recovery %s: %w", stage, err)
	default:
		return err
	}
}
