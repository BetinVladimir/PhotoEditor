package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

func (s SQLTransferSourceReader) nativeInputDefectSavedParamsObject(ctx context.Context, who auth.Identity, request Request, ref v2input.Ref) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || who.UserID != request.ActorID {
		return zero, v2input.ErrInvalid
	}
	var object objectstore.Object
	var actual v2input.Ref
	var bucket string
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_native_input_defect_saved_params($1,$2,$3,$4,$5,$6,$7)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID, ref.ArtifactID, ref.SHA256).Scan(&actual.ArtifactID, &actual.SHA256, &bucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType)
	})
	if err != nil {
		return zero, err
	}
	if actual != ref || bucket != s.Bucket || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.Bytes < 2 || object.Bytes > 1<<20 || object.ContentType != "application/json" {
		return zero, v2input.ErrConflict
	}
	object.SHA256 = actual.SHA256
	return object, nil
}

// Reads original accepted Params for later full producer reconstruction; this
// packet grants no result, qualification or admission authority.
func (s SQLTransferSourceReader) readNativeInputDefectSavedParams(ctx context.Context, who auth.Identity, request Request, intent, snapshot v2input.Ref) (nativeInputDefectSavedLocator, []byte, error) {
	var zero nativeInputDefectSavedLocator
	saved, err := s.readNativeInputDefectSavedLocator(ctx, who, request, intent, snapshot)
	if err != nil {
		return zero, nil, err
	}
	object, err := s.nativeInputDefectSavedParamsObject(ctx, who, request, saved.params)
	if err != nil {
		return zero, nil, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, saved.params)
	if err != nil {
		return zero, nil, err
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || string(normalized) != string(raw) {
		return zero, nil, v2input.ErrInvalid
	}
	current, err := s.readNativeInputDefectSavedLocator(ctx, who, request, intent, snapshot)
	if err != nil {
		return zero, nil, err
	}
	if !reflect.DeepEqual(current, saved) {
		return zero, nil, v2input.ErrConflict
	}
	after, err := s.nativeInputDefectSavedParamsObject(ctx, who, request, saved.params)
	if err != nil {
		return zero, nil, err
	}
	if after != object {
		return zero, nil, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, nil, err
	}
	return saved, raw, nil
}
