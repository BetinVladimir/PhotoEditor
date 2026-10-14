package interchange

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// The occurrence index belongs to the physically pinned parent chain. Fields
// may be reused by later operations, so identical parent producer is not required.
type ExportDenseWarpOccurrenceAuthority interface {
	QualifyExportDenseWarpOccurrence(context.Context, v2input.Entry, []byte, int, v2input.Entry) error
}

func validateExportDenseWarpOccurrence(parent v2input.Entry, raw []byte, index int, field v2input.Entry) error {
	if !validImportRef(parent.Ref) || !validImportRef(field.Ref) || field.Contract != "DenseWarpField" || index < 0 || len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != parent.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	var chain json.RawMessage
	switch parent.Contract {
	case "WorkingImage":
		image, err := review.ParseWorkingImage(raw)
		if err != nil {
			return err
		}
		chain = image.TransformChain
	case "MaskRef":
		mask, err := review.ParseMaskRef(raw)
		if err != nil {
			return err
		}
		chain = mask.TransformChain
	default:
		return v2input.ErrInvalid
	}
	var parsed struct {
		Steps []struct {
			Kind    string              `json:"kind"`
			Inverse *review.ArtifactRef `json:"inverse_field"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(chain, &parsed); err != nil {
		return err
	}
	if index >= len(parsed.Steps) {
		return v2input.ErrInvalid
	}
	step := parsed.Steps[index]
	if step.Kind != "dense_warp" || step.Inverse == nil || step.Inverse.ID != field.Ref.ArtifactID || step.Inverse.SHA256 != field.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	return nil
}
func (s SQLExportDocumentCatalog) QualifyExportDenseWarpOccurrence(ctx context.Context, parent v2input.Entry, raw []byte, index int, field v2input.Entry) error {
	return qualifyExportDenseWarpOccurrence(ctx, s, s.DB, s.Tenant, s.Who, s.Request.ProjectID, s.Bucket, parent, raw, index, field)
}
func (s SQLExportResultInputCatalog) QualifyExportDenseWarpOccurrence(ctx context.Context, parent v2input.Entry, raw []byte, index int, field v2input.Entry) error {
	config := s.Export
	return qualifyExportDenseWarpOccurrence(ctx, s, config.DB, config.Tenant, config.Who, config.Request.ProjectID, config.Bucket, parent, raw, index, field)
}
func qualifyExportDenseWarpOccurrence(ctx context.Context, catalog ExportDocumentCatalog, db *pgxpool.Pool, tenantID uuid.UUID, who auth.Identity, project uuid.UUID, bucketName string, parent v2input.Entry, raw []byte, index int, field v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if catalog == nil || db == nil || tenantID == uuid.Nil || project == uuid.Nil || len(bucketName) < 3 {
		return v2input.ErrInvalid
	}
	if err := validateExportDenseWarpOccurrence(parent, raw, index, field); err != nil {
		return err
	}
	parentObject, err := catalog.LookupExportDocument(ctx, parent)
	if err != nil {
		return err
	}
	object, err := catalog.LookupExportDocument(ctx, field)
	if err != nil {
		return err
	}
	if !exportArrayMediaType(object.ContentType) {
		return v2input.ErrInvalid
	}
	err = tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(
   SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   JOIN photo.artifacts f ON f.tenant_id=v.tenant_id AND f.id=$5 AND f.sha256=$6
   WHERE v.tenant_id=$1 AND a.project_id=$2 AND a.deleted_at IS NULL AND a.upload_state='verified'
    AND v.review_state='accepted' AND f.output_role='warp_field'
    AND f.bucket=$10 AND f.object_key=$11 AND f.object_version=$12 AND f.bytes=$13 AND f.media_type=$14
    AND ($7<>'WorkingImage' OR (v.master_artifact_id=$3 AND v.content_hash=$4))
    AND photo.v2_draft_artifact_scope($1,$2,v.id,$3,$4,$7,$8,$9,NULL,NULL)
    AND photo.v2_draft_artifact_scope($1,$2,v.id,$5,$6,'DenseWarpField',$8,$9,$3,$4)
  )`, tenantID, project, parent.Ref.ArtifactID, parent.Ref.SHA256, field.Ref.ArtifactID, field.Ref.SHA256, parent.Contract, who.UserID, who.SessionID, bucketName, object.Key, object.VersionID, object.Bytes, object.ContentType).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return err
	}
	currentParent, err := catalog.LookupExportDocument(ctx, parent)
	if err != nil {
		return err
	}
	if currentParent != parentObject {
		return v2input.ErrInvalid
	}
	currentField, err := catalog.LookupExportDocument(ctx, field)
	if err != nil {
		return err
	}
	if currentField != object {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportDenseWarpOccurrenceAuthority = SQLExportDocumentCatalog{}
var _ ExportDenseWarpOccurrenceAuthority = SQLExportResultInputCatalog{}
