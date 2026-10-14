package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Registers only after immutable publication. No object/queue IO occurs inside
// this retryable transaction; qualification and parent graph admission are separate.
func (s SQLTransferSourceReader) registerNativeDefectProtection(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, prepared nativeDefectProtectionPreparation, root v2input.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(prepared.body))
	if scheduler == nil || prepared.document == uuid.Nil || prepared.body == "" || prepared.report == "" || root.ArtifactID == uuid.Nil || root.SHA256 != hex.EncodeToString(digest[:]) || prepared.pixels != prepared.parent.result.origin.pins.Protection {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		if err := s.lockNativeDefectContinuation(ctx, tx, who, tenantID, binding, prepared.parent); err != nil {
			return err
		}
		var ready bool
		proof := prepared.parent.result
		if err := tx.QueryRow(ctx, "SELECT photo.register_semantic_native_protection($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14,$15)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, proof.task, proof.catalog.ResultID, prepared.parent.anchor, prepared.document, root.ArtifactID, root.SHA256, prepared.body, prepared.report).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
		return ctx.Err()
	})
}
