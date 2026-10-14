package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Publishes/registers the exact report-derived protected root. Partial durable
// publication/registration is recoverable with the same key/task/document ID.
// No object IO spans SQL TX. This proves content/provenance, not model policy,
// candidate approval, transfer admission or parent graph activation.
func (s SQLTransferSourceReader) PublishRegisteredNativeDefectProtection(ctx context.Context, who auth.Identity, tenantID uuid.UUID, key string, raw []byte, intent, snapshot v2input.Ref, task uuid.UUID, publisher v2draft.Publisher, scheduler *pgxpool.Pool) (VerifiedProtection, error) {
	var zero VerifiedProtection
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.ProjectID == uuid.Nil || s.Bucket == "" || scheduler == nil || publisher.DB == nil || publisher.Objects == nil || publisher.Bucket != s.Bucket {
		return zero, v2input.ErrInvalid
	}
	request, err := CompileRequest(tenantID, who.UserID, key, raw)
	if err != nil {
		return zero, err
	}
	prepared, err := s.prepareNativeDefectProtection(ctx, who, request, key, intent, snapshot, task)
	if err != nil {
		return zero, err
	}
	image, err := s.Read(ctx, who, tenantID, request.Intent.Reference)
	if err != nil {
		return zero, err
	}
	root, err := publisher.PublishSemanticProtectionDocument(ctx, who, tenantID, prepared.document, request.Intent.Reference, image, []byte(prepared.body))
	if err != nil {
		return zero, err
	}
	if err := s.registerNativeDefectProtection(ctx, scheduler, who, tenantID, request.Intent.Reference, prepared, root); err != nil {
		return zero, err
	}
	verified, err := s.ReadRegisteredNativeDefectProtection(ctx, who, request, key, intent, snapshot, task, root)
	if err != nil {
		return zero, err
	}
	if verified.Pixels != prepared.pixels || string(verified.Body) != prepared.body {
		return zero, v2input.ErrConflict
	}
	return verified, nil
}
