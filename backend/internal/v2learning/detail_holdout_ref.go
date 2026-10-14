package v2learning

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// PortraitDetailHoldoutRef identifies one event's immutable, recovery-stable
// producer evidence within the training run. SQL reservation must use the
// same UUIDv5 name and verify the pinned content digest independently.
func PortraitDetailHoldoutRef(runID, eventID uuid.UUID, sha string) (v2input.Ref, error) {
	if runID == uuid.Nil || eventID == uuid.Nil || !validSHA(sha) {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	return v2input.Ref{ArtifactID: uuid.NewSHA1(runID,
		[]byte("v23-detail/"+eventID.String())), SHA256: sha}, nil
}
