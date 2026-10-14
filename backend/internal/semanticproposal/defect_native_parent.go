package semanticproposal

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type nativeDefectParentProof struct {
	anchor string
	result nativeDefectAdmissionProof
}

// Parent binding precedes object IO and is repeated after complete verification.
// This reader does not admit/activate a parent or authorize candidate review.
func (s SQLTransferSourceReader) ReadNativeDefectResultForRequest(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID) (VerifiedNativeDefectResult, error) {
	var zero VerifiedNativeDefectResult
	owned, err := CompileRequest(request.TenantID, who.UserID, key, request.Body)
	if err != nil {
		return zero, err
	}
	body, err := canonical.Marshal(request.Intent)
	if err != nil || who.UserID != request.ActorID || who.SessionID == uuid.Nil || task == uuid.Nil || owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA || !bytes.Equal(body, owned.Body) || intent.ArtifactID == uuid.Nil || snapshot.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	sha, err := canonical.Hash(owned.Intent)
	if err != nil || intent.SHA256 != sha {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(snapshot.SHA256); err != nil {
		return zero, err
	}
	raw, err := canonical.Marshal(map[string]any{"parent_run_id": owned.RunID, "request_sha": owned.RequestSHA, "auth_session_id": who.SessionID, "source": owned.Intent.Reference, "intent": intent, "snapshot": snapshot})
	if err != nil {
		return zero, err
	}
	anchor := string(raw)
	check := func() error {
		return tenant.WithTenant(ctx, s.DB, who, owned.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
			var ready bool
			if err := tx.QueryRow(ctx, "SELECT photo.semantic_defect_native_parent_ready($1,$2,$3,$4,$5,$6::jsonb)", owned.TenantID, s.ProjectID, who.UserID, who.SessionID, task, anchor).Scan(&ready); err != nil {
				return err
			}
			if !ready {
				return v2input.ErrConflict
			}
			return nil
		})
	}
	if s.DB == nil || s.ProjectID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if err := check(); err != nil {
		return zero, err
	}
	result, err := s.ReadNativeDefectResultEvidence(ctx, who, owned.TenantID, owned.Intent.Reference, task)
	if err != nil {
		return zero, err
	}
	if err := check(); err != nil {
		return zero, err
	}
	result.parentProof = nativeDefectParentProof{anchor: anchor, result: result.admissionProof}
	return result, nil
}

// Caller composes qualification and full parent admission in this same TX.
func (s SQLTransferSourceReader) lockNativeDefectContinuation(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, proof nativeDefectParentProof) error {
	if proof.anchor == "" {
		return v2input.ErrInvalid
	}
	if err := s.lockNativeDefectResult(ctx, tx, who, tenantID, binding, proof.result); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_native_parent($1,$2,$3,$4,$5,$6::jsonb)", tenantID, s.ProjectID, who.UserID, who.SessionID, proof.result.task, proof.anchor).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
