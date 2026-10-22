package v2draft

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// GetLayerStack resolves an artifact ref within the authorized current asset project.
// The returned immutable document must bind to the current verified WorkingImage.
func (s Service) GetLayerStack(ctx context.Context, who auth.Identity, tid, assetID uuid.UUID, ref v2input.Ref) (json.RawMessage, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if assetID == uuid.Nil || !isDocumentRef(ref) {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	binding, err := (BindingResolver{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket}).Current(ctx, who, tid, assetID)
	if err != nil {
		return nil, err
	}
	var documentID uuid.UUID
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		streamID, e := currentDraftStream(ctx, tx, who, tid, assetID, role)
		if e != nil {
			return e
		}
		e = tx.QueryRow(ctx, `SELECT d.id FROM photo.v2_document_streams s
   JOIN photo.v2_documents d ON d.tenant_id=s.tenant_id AND d.project_id=s.project_id
   JOIN photo.artifacts ar ON ar.tenant_id=d.tenant_id AND ar.id=d.artifact_id AND ar.sha256=d.sha256
   WHERE s.tenant_id=$1 AND s.id=$2 AND s.asset_version_id=$3
   AND d.document_type='LayerStack' AND d.artifact_id=$4 AND d.sha256=$5
   AND ar.state='ready' AND ar.verified_at IS NOT NULL AND ar.object_version IS NOT NULL
   AND ar.object_version NOT IN ('','null') AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
   ORDER BY d.id LIMIT 1`, tid, streamID, binding.AssetVersionID, ref.ArtifactID, ref.SHA256).Scan(&documentID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		return e
	})
	if err != nil {
		return nil, err
	}
	body, err := s.GetDocument(ctx, who, tid, documentID)
	if err != nil {
		return nil, err
	}
	if _, err = ValidateLayerStack(body, binding); err != nil {
		return nil, domain.NewError("DOCUMENT_INVALID", 422)
	}
 current,err:=(BindingResolver{DB:s.DB,Objects:s.Objects,Bucket:s.Bucket}).Current(ctx,who,tid,assetID)
 if err!=nil{return nil,err};if current!=binding{return nil,domain.NewError("REVISION_CONFLICT",409)}
 if err=ctx.Err();err!=nil{return nil,err}
 return body, nil
}
