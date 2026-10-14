package interchange

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Geometry alone is not producer authority. A native export installation must
// prove the exact parent/array producer relation before probing array bytes.
type ExportWorkingArrayAuthority interface {
	QualifyExportWorkingArray(context.Context, v2input.Entry, v2input.Entry) error
}

func (s SQLExportDocumentCatalog) QualifyExportWorkingArray(ctx context.Context, parent, array v2input.Entry) error {
	return qualifyExportWorkingArray(ctx, s, s.DB, s.Tenant, s.Who, s.Request.ProjectID, parent, array)
}
func (s SQLExportResultInputCatalog) QualifyExportWorkingArray(ctx context.Context, parent, array v2input.Entry) error {
	config := s.Export
	return qualifyExportWorkingArray(ctx, s, config.DB, config.Tenant, config.Who, config.Request.ProjectID, parent, array)
}
func qualifyExportWorkingArray(ctx context.Context, documents ExportDocumentCatalog, db *pgxpool.Pool, tenantID uuid.UUID, who auth.Identity, projectID uuid.UUID, parent, array v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if documents == nil || db == nil || tenantID == uuid.Nil || projectID == uuid.Nil {
		return v2input.ErrInvalid
	}
	if parent.Contract != "WorkingImage" || (array.Contract != "WorkingPixels" && array.Contract != "WorkingAlpha") || !validImportRef(parent.Ref) || !validImportRef(array.Ref) {
		return v2input.ErrInvalid
	}
	// Both objects must remain readable in this pending, frozen export request.
	parentObject, err := documents.LookupExportDocument(ctx, parent)
	if err != nil {
		return err
	}
	arrayObject, err := documents.LookupExportDocument(ctx, array)
	if err != nil {
		return err
	}
	err = tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(
   SELECT 1 FROM photo.asset_versions v JOIN photo.assets a
    ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   WHERE v.tenant_id=$1 AND a.project_id=$2 AND a.deleted_at IS NULL
    AND a.upload_state='verified' AND v.review_state='accepted'
    AND v.master_artifact_id=$3 AND v.content_hash=$4
    AND photo.v2_draft_artifact_scope($1,$2,v.id,$5,$6,$7,$8,$9,$3,$4)
  )`, tenantID, projectID, parent.Ref.ArtifactID, parent.Ref.SHA256, array.Ref.ArtifactID, array.Ref.SHA256, array.Contract, who.UserID, who.SessionID).Scan(&allowed)
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
	currentParent, err := documents.LookupExportDocument(ctx, parent)
	if err != nil {
		return err
	}
	if currentParent != parentObject {
		return v2input.ErrInvalid
	}
	currentArray, err := documents.LookupExportDocument(ctx, array)
	if err != nil {
		return err
	}
	if currentArray != arrayObject {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportWorkingArrayAuthority = SQLExportDocumentCatalog{}
var _ ExportWorkingArrayAuthority = SQLExportResultInputCatalog{}
