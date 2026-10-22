package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type nativeInitialRecoveryProbeError struct{}

func (*nativeInitialRecoveryProbeError) Error() string { return "closed probe" }
func TestNativeInitialRecoveryStageErrorPreservesCause(t *testing.T) {
	for _, stage := range []string{"validate", "all_native_ticket_identity", "detector_ids", "prepared_ticket_identity", "initial_ticket_identity", "manual_presence_invalid", "read_terminal", "load_anchor", "resolve_catalog", "read_all_native_parent", "resume_all_native_parent", "restore_all_native_producers", "read_target_checkpoint", "recover_target_checkpoint", "ensure_initial_person", "advance_face_pipeline", "read_person_state", "classify_person_state", "restore_person_plan", "read_person_input", "read_manual_presence", "advance_initial_defect", "save_target_checkpoint", "read_manual_selection", "read_manual_terminal", "resume_manual_defect", "advance_target_persons", "advance_target_detectors", "advance_target_faces", "advance_target_skins", "advance_target_defects", "advance_scene_prerequisites", "admit_prepared_parent", "admit_initial_parent"} {
		for _, original := range []error{v2input.ErrConflict, context.Canceled, context.DeadlineExceeded, &nativeInitialRecoveryProbeError{}} {
			got := nativeInitialRecoveryStageError(stage, original)
			if !errors.Is(got, original) || !strings.HasPrefix(got.Error(), "native initial recovery "+stage+": ") {
				t.Fatal("stage lost original error", stage)
			}
			var typed *nativeInitialRecoveryProbeError
			if _, ok := original.(*nativeInitialRecoveryProbeError); ok && (!errors.As(got, &typed) || typed != original) {
				t.Fatal("typed original replaced")
			}
		}
		if nativeInitialRecoveryStageError(stage, nil) != nil {
			t.Fatal("nil became failure")
		}
	}
	original := errors.New("original")
	if nativeInitialRecoveryStageError("untrusted-label", original) != original {
		t.Fatal("unknown label changed original")
	}
}
