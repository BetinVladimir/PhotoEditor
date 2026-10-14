package v2learning

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ReviewedDustHoldoutRef is stable across an ambiguous publication result.
// The future SQL document reservation must use this exact UUIDv5 name.
func ReviewedDustHoldoutRef(runID, eventID uuid.UUID, sha string) (v2input.Ref, error) {
	if runID == uuid.Nil || eventID == uuid.Nil || !validSHA(sha) {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	return v2input.Ref{ArtifactID: uuid.NewSHA1(runID,
		[]byte("v23-dust/"+eventID.String())), SHA256: sha}, nil
}
