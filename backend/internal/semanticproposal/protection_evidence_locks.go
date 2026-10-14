package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Selected protected mask producer/pins only; policy/coverage and input
// declaration barrier/recheck remain caller obligations. Roll back on error.
func (s SQLTransferSourceReader) lockProtectionPreparationProof(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, p protectionPreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !validProtectionPreparationScope(s, who, tenant, binding, p) {
		return v2input.ErrInvalid
	}
	entries := make([]manualEvidencePin, 0, 2)
	// Lexicographic role order is the SQL comparison contract.
	for i, pin := range [2]defectEvidenceObject{p.pixels, p.root} {
		entries = append(entries, manualEvidencePin{[2]string{"protected_pixels", "protected_ref"}[i], pin.ref.ArtifactID, pin.ref.SHA256, p.bucket, pin.object.Key, pin.object.VersionID, pin.object.Bytes, pin.object.ContentType})
	}
	body, err := canonical.Marshal(entries)
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_protection_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, p.root.ref.ArtifactID, p.root.ref.SHA256, p.pixels.ref.ArtifactID, p.pixels.ref.SHA256, string(body)).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return nil
}
