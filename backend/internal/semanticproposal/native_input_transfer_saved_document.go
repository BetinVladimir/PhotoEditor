package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

func (s SQLTransferSourceReader) nativeInputTransferSavedDocumentObject(ctx context.Context, who auth.Identity, request Request, ordinal int, ref v2input.Ref) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || ordinal < -1 || ordinal > 4999 || who.UserID != request.ActorID {
		return zero, v2input.ErrInvalid
	}
	var object objectstore.Object
	var actual v2input.Ref
	var bucket string
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_native_input_transfer_saved_document($1,$2,$3,$4,$5,$6,$7,$8)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID, ordinal, ref.ArtifactID, ref.SHA256).Scan(&actual.ArtifactID, &actual.SHA256, &bucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType)
	})
	if err != nil {
		return zero, err
	}
	if actual != ref || bucket != s.Bucket || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.Bytes < 2 || object.Bytes > 8<<20 || object.ContentType != "application/json" {
		return zero, v2input.ErrConflict
	}
	object.SHA256 = actual.SHA256
	return object, nil
}

func (s SQLTransferSourceReader) readNativeInputTransferSavedSnapshot(ctx context.Context, who auth.Identity, request Request, saved nativeInputTransferSavedMetadata, budget int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	object, err := s.nativeInputTransferSavedDocumentObject(ctx, who, request, -1, saved.snapshot)
	if err != nil {
		return nil, err
	}
	var used int64
	if budget <= 0 || budget > 9007199254740991 {
		return nil, v2input.ErrInvalid
	}
	if err := chargeMetadata(&used, budget, int(object.Bytes)); err != nil {
		return nil, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, saved.snapshot)
	if err != nil {
		return nil, err
	}
	snapshot, err := v2input.DecodePinnedSessionInputSnapshot(saved.snapshot, raw)
	if err != nil {
		return nil, err
	}
	if snapshot.SnapshotID != request.Intent.TargetSnapshotID || snapshot.SnapshotDigest != saved.snapshotDigest || len(snapshot.Items) != len(saved.params) {
		return nil, v2input.ErrConflict
	}
	after, err := s.nativeInputTransferSavedDocumentObject(ctx, who, request, -1, saved.snapshot)
	if err != nil {
		return nil, err
	}
	if after != object {
		return nil, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
