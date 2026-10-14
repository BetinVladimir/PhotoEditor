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

func (s SQLTransferSourceReader) nativeInputSavedIntentObject(ctx context.Context, who auth.Identity, request Request) (defectEvidenceObject, error) {
	var zero, pin defectEvidenceObject
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || s.ProjectID == uuid.Nil || who.UserID != request.ActorID || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	var bucket string
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_native_input_saved_intent($1,$2,$3,$4,$5,$6)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID, request.Intent.Reference.AssetVersionID).Scan(&pin.ref.ArtifactID, &pin.ref.SHA256, &bucket, &pin.object.Key, &pin.object.VersionID, &pin.object.Bytes, &pin.object.ContentType)
	})
	if err != nil {
		return zero, err
	}
	sha, err := canonical.Hash(request.Intent)
	if err != nil {
		return zero, err
	}
	if pin.ref.ArtifactID == uuid.Nil || pin.ref.SHA256 != sha || bucket != s.Bucket || pin.object.Key == "" || pin.object.VersionID == "" || pin.object.VersionID == "null" || pin.object.Bytes < 2 || pin.object.Bytes > 1<<20 || pin.object.ContentType != "application/json" {
		return zero, v2input.ErrConflict
	}
	pin.object.SHA256 = pin.ref.SHA256
	return pin, nil
}

// Reads the original published bytes plus complete current target inventory.
// No document publication or replacement occurs. Admission must still lock both.
func (s SQLTransferSourceReader) readNativeInputSavedPublishedIntent(ctx context.Context, who auth.Identity, request Request, budget int64) (PublishedIntent, error) {
	var zero PublishedIntent
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if budget <= 0 || budget > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(request.Intent)
	if err != nil || !bytes.Equal(body, request.Body) {
		return zero, v2input.ErrInvalid
	}
	before, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return zero, err
	}
	if before.Scope.ProjectID != s.ProjectID {
		return zero, v2input.ErrConflict
	}
	pin, err := s.nativeInputSavedIntentObject(ctx, who, request)
	if err != nil {
		return zero, err
	}
	var used int64
	if err := chargeMetadata(&used, budget, len(before.Targets)*512); err != nil {
		return zero, err
	}
	if err := chargeMetadata(&used, budget, int(pin.object.Bytes)); err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(raw, body) {
		return zero, v2input.ErrConflict
	}
	after, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return zero, err
	}
	current, err := s.nativeInputSavedIntentObject(ctx, who, request)
	if err != nil {
		return zero, err
	}
	if !sameTransferInventory(before, after) || current != pin {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return PublishedIntent{Ref: pin.ref, Inventory: after}, nil
}
