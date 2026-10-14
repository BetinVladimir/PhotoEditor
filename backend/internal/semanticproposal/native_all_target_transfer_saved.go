package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Saved locators only. Caller independently restores original reference and all
// targets, recompiles the complete graph and repeats live locks before admission.
func (s SQLTransferSourceReader) decodeNativeAllTargetTransferSaved(ctx context.Context, who auth.Identity, request Request, raw json.RawMessage, sha string) (nativeAllTargetTransferConfig, error) {
	var zero nativeAllTargetTransferConfig
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	saved, err := decodeNativeAllTargetTransferConfig(raw, sha)
	if err != nil {
		return zero, err
	}
	if request.ActorID != who.UserID || saved.ProjectID != s.ProjectID || saved.SnapshotID != request.Intent.TargetSnapshotID || saved.Semantic.AuthSessionID != who.SessionID || saved.Native.Parent != request.RunID || saved.Native.RequestSHA != request.RequestSHA || saved.Native.Reference != request.Intent.Reference {
		return zero, v2input.ErrConflict
	}
	return saved, ctx.Err()
}

func (s SQLTransferSourceReader) readNativeAllTargetTransferSaved(ctx context.Context, who auth.Identity, request Request) (json.RawMessage, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if s.DB == nil || who.UserID != request.ActorID {
		return nil, "", v2input.ErrInvalid
	}
	var raw json.RawMessage
	var sha string
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT config_snapshot,config_hash FROM photo.semantic_native_targets_transfer_saved_config($1,$2,$3,$4,$5,$6,$7)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID, request.RequestSHA, request.Intent.TargetSnapshotID).Scan(&raw, &sha)
	})
	if err != nil {
		return nil, "", err
	}
	if _, err := s.decodeNativeAllTargetTransferSaved(ctx, who, request, raw, sha); err != nil {
		return nil, "", err
	}
	return append(json.RawMessage(nil), raw...), sha, ctx.Err()
}

// Routing hint only; full saved decoding and physical/SQL revalidation follow.
func (s SQLTransferSourceReader) hasNativeAllTargetSavedParent(ctx context.Context, who auth.Identity, request Request) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || who.UserID != request.ActorID {
		return false, v2input.ErrInvalid
	}
	var exists bool
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM photo.semantic_native_targets_transfer_saved_config($1,$2,$3,$4,$5,$6,$7))", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID, request.RequestSHA, request.Intent.TargetSnapshotID).Scan(&exists)
	})
	if err != nil {
		return false, err
	}
	return exists, ctx.Err()
}
