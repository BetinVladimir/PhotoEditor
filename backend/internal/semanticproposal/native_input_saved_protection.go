package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type nativeInputSavedProtection struct {
	root             v2input.Ref
	document, result uuid.UUID
}

func (s SQLTransferSourceReader) readNativeInputSavedProtection(ctx context.Context, who auth.Identity, prepared nativeInputDefectProtectionPreparation) (nativeInputSavedProtection, error) {
	var zero nativeInputSavedProtection
	if err := verifyNativeDefectDraftSeal(ctx, prepared.plan); err != nil {
		return zero, err
	}
	if s.DB == nil || len(prepared.plan.shell.Tasks) != 1 || prepared.result.catalog.ResultID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	r := prepared.plan.prepared.dependencies.preparation.landmarksPlan.detector.request
	var saved nativeInputSavedProtection
	err := tenant.WithTenant(ctx, s.DB, who, r.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT root_id,root_sha,document_id,result_id FROM photo.semantic_native_input_saved_protection($1,$2,$3,$4,$5,$6)", r.TenantID, s.ProjectID, who.UserID, who.SessionID, r.RunID, prepared.plan.shell.Tasks[0].ID).Scan(&saved.root.ArtifactID, &saved.root.SHA256, &saved.document, &saved.result)
	})
	if err != nil {
		return zero, err
	}
	if err := verifyNativeInputSavedProtection(saved, prepared); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return saved, nil
}

func verifyNativeInputSavedProtection(saved nativeInputSavedProtection, prepared nativeInputDefectProtectionPreparation) error {
	if len(prepared.body) < 2 || len(prepared.body) > 1<<20 || prepared.document == uuid.Nil || prepared.result.catalog.ResultID == uuid.Nil {
		return v2input.ErrInvalid
	}
	sha := sha256.Sum256([]byte(prepared.body))
	if saved.root.ArtifactID == uuid.Nil || saved.root.SHA256 != hex.EncodeToString(sha[:]) || saved.document != prepared.document || saved.result != prepared.result.catalog.ResultID {
		return v2input.ErrConflict
	}
	return nil
}

// Uses only an existing registered root. Original report/candidates/protection
// bytes are reverified before lookup and again through the registered reader.
func (s SQLTransferSourceReader) restoreRegisteredNativeInputProtection(ctx context.Context, who auth.Identity, plan nativeDefectDraftPlan, color TransferColorProfile) (registeredNativeInputProtection, error) {
	var zero registeredNativeInputProtection
	prepared, err := s.prepareNativeInputDefectProtection(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	saved, err := s.readNativeInputSavedProtection(ctx, who, prepared)
	if err != nil {
		return zero, err
	}
	registered, err := s.readRegisteredNativeInputProtection(ctx, who, publishedNativeInputDefectProtection{root: saved.root, preparation: prepared}, color)
	if err != nil {
		return zero, err
	}
	after, err := s.readNativeInputSavedProtection(ctx, who, prepared)
	if err != nil {
		return zero, err
	}
	if saved != after {
		return zero, v2input.ErrConflict
	}
	return registered, nil
}
