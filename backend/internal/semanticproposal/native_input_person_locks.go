package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Source and native protection are locked by the caller in the same transaction.
// This gate repeats independent person evidence, qualification and unique candidate
// discovery under the tenant inventory barrier; no generic protection proof is used.
func (s SQLTransferSourceReader) lockNativeInputPersonEvidence(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, proof nativeInputPersonProtectionProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var expected v2qualification.Expected
	var report personEvidencePin
	if tx == nil || proof.root.ArtifactID == uuid.Nil || proof.pixels.ArtifactID == uuid.Nil || proof.count < 0 || proof.evidence == "" || proof.qualification == "" || json.Unmarshal([]byte(proof.expected), &expected) != nil || expected.Operation != "mask.person" || expected.Version != "1" || json.Unmarshal([]byte(proof.qualification), &report) != nil || report.ArtifactID != proof.report.ArtifactID || report.SHA256 != proof.report.SHA256 {
		return v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_inventory($1,$2,$3,$4)", request.TenantID, s.ProjectID, who.UserID, who.SessionID).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	b := request.Intent.Reference
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, proof.root.ArtifactID, proof.root.SHA256, proof.pixels.ArtifactID, proof.pixels.SHA256, proof.expected, proof.evidence).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_qualification($1,$2,$3,$4,$5,$6)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, proof.expected, proof.qualification).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	if err := tx.QueryRow(ctx, "SELECT photo.semantic_proposal_person_candidate_matches($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, proof.root.ArtifactID, proof.root.SHA256, proof.pixels.ArtifactID, proof.pixels.SHA256, proof.expected).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
