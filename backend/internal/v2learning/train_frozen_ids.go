package v2learning

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// TrainingFrozenRef is computable before the learning draft exists, breaking
// the manifest/artifact publication cycle. SQL reservation must use the same
// UUIDv5(run, purpose/event) derivation and compare the SHA with recovery.
func TrainingFrozenRef(runID, eventID uuid.UUID, side, sha string) (
	v2input.Ref, error) {
	if runID == uuid.Nil || eventID == uuid.Nil ||
		(side != "base" && side != "approved") || !validSHA(sha) {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	id := uuid.NewSHA1(runID, []byte("v23-frozen/"+side+"/"+eventID.String()))
	return v2input.Ref{ArtifactID: id, SHA256: sha}, nil
}

func (c CompiledTrainingRecipes) Refs(runID, eventID uuid.UUID) (
	v2input.Ref, v2input.Ref, error) {
	base, err := TrainingFrozenRef(runID, eventID, "base", c.BaseSHA)
	if err != nil {
		return v2input.Ref{}, v2input.Ref{}, err
	}
	approved, err := TrainingFrozenRef(runID, eventID, "approved",
		c.ApprovedSHA)
	return base, approved, err
}
