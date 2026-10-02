package v2learning

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

type SQLTrainingMaskProvenance struct {
	DB *pgxpool.Pool // photo_scheduler
}

func (s SQLTrainingMaskProvenance) Verify(ctx context.Context,
	start TrainingStart, candidate TrainingCandidate,
	root, child v2input.Ref, protected bool) error {
	if s.DB == nil || start.TenantID == uuid.Nil ||
		start.ProjectID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil || candidate.EventID == uuid.Nil ||
		!validManifestRef(root) || !validManifestRef(child) {
		return ErrInvalidTrainingDraft
	}
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT photo.v23_training_mask_provenance(
	 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, start.TenantID,
		start.ProjectID, candidate.EventID, root.ArtifactID, root.SHA256,
		child.ArtifactID, child.SHA256, protected, start.ActorID,
		start.AuthSessionID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrTrainingDraftConflict
	}
	return nil
}

var _ TrainingMaskProvenanceVerifier = SQLTrainingMaskProvenance{}
