package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Stable reference detector stage identity belongs to one parent request run.
// Retry reuses this identity; SQL must still compare request/config hashes.
func detectorPreparationIDs(parent uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if parent == uuid.Nil {
		return uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	run := uuid.NewSHA1(parent, []byte("semantic.face.detect:reference"))
	workflow, err := v2input.WorkflowID(run)
	return run, workflow, err
}

func verifyDetectorStageIdentity(parent uuid.UUID, prepared sourceDetectorPreparation) error {
	_, workflow, err := detectorPreparationIDs(parent)
	if err != nil {
		return err
	}
	expected, err := v2input.TaskID(workflow, "face.detect", 0)
	if err != nil {
		return err
	}
	draft, err := prepared.frozen.DraftTask()
	if err != nil {
		return err
	}
	if draft.ID != expected {
		return v2input.ErrConflict
	}
	return nil
}
