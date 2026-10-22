package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type checkpointStageCause struct{}

func (*checkpointStageCause) Error() string { return "original cause" }
func TestNativeTargetCheckpointStageErrorPreservesCause(t *testing.T) {
	for _, stage := range []string{"restore_manifest", "restore_face_producers", "read_manual_presence", "manual_selection_present", "read_defect_params", "missing_person_locator", "decode_defect_params", "restore_defect_plan", "restore_registered_protection", "advance_target_persons", "advance_target_detectors", "advance_target_faces", "advance_target_skins", "advance_target_defects", "advance_scene_prerequisites", "admit_parent", "parent_ticket_identity"} {
		t.Run(stage, func(t *testing.T) {
			if nativeTargetCheckpointStageError(stage, nil) != nil {
				t.Fatal("nil became error")
			}
			cause := &checkpointStageCause{}
			wrapped := nativeTargetCheckpointStageError(stage, cause)
			var typed *checkpointStageCause
			if !errors.Is(wrapped, cause) || !errors.As(wrapped, &typed) || typed != cause || !strings.HasPrefix(wrapped.Error(), "native target checkpoint "+stage+": ") {
				t.Fatal("cause/closed stage lost")
			}
			for _, err := range []error{context.Canceled, v2input.ErrConflict, v2input.ErrInvalid} {
				if !errors.Is(nativeTargetCheckpointStageError(stage, err), err) {
					t.Fatal("classification changed")
				}
			}
		})
	}
	cause := errors.New("original")
	if nativeTargetCheckpointStageError("untrusted arbitrary label", cause) != cause {
		t.Fatal("unknown label was interpolated")
	}
}
