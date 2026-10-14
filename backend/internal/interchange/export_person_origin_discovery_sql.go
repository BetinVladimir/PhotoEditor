package interchange

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// PortablePersonOriginRootDiscoverer resolves the artifact identity of an
// inline MaskRef. The full producer, physical bytes and recipe occurrence are
// independently requalified by PortablePersonOriginIssuer before signing.
type PortablePersonOriginRootDiscoverer interface {
	DiscoverPortablePersonOriginRoot(context.Context, v2input.SessionVersionBinding,
		v2input.Ref, string) (v2input.Ref, error)
}

func (s SQLExportDocumentCatalog) DiscoverPortablePersonOriginRoot(ctx context.Context,
	source v2input.SessionVersionBinding, pixels v2input.Ref, rootSHA string) (v2input.Ref, error) {
	var zero, found v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Tenant == uuid.Nil || s.Request.ProjectID == uuid.Nil ||
		source.AssetVersionID == uuid.Nil || !validImportRef(source.Source) ||
		!portablePersonOriginSHA.MatchString(source.CanvasHash) || !validImportRef(pixels) ||
		!portablePersonOriginSHA.MatchString(rootSHA) {
		return zero, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT root.id,root.sha256
 FROM photo.artifacts mask
 JOIN photo.tasks producer ON producer.tenant_id=mask.tenant_id AND producer.id=mask.created_by_task_id
 JOIN photo.runs run ON run.tenant_id=producer.tenant_id AND run.id=producer.run_id
 JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id AND root.created_by_task_id=producer.id
 JOIN photo.asset_versions version ON version.tenant_id=producer.tenant_id AND version.id=$4
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 WHERE mask.tenant_id=$1 AND mask.id=$2 AND mask.sha256=$3 AND mask.output_role='person_mask'
 AND root.sha256=$5 AND root.output_role='person_mask_ref' AND root.media_type='application/json'
 AND mask.produced_attempt=producer.attempt AND mask.produced_fence=producer.fencing_token
 AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
 AND producer.operation='mask.person' AND producer.operation_version='1' AND producer.state='succeeded'
 AND producer.canonical_result_id IS NOT NULL AND NOT run.cancel_requested
 AND version.master_artifact_id=$6 AND version.content_hash=$7 AND asset.project_id=$8
 AND version.review_state='accepted' AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND mask.state='ready' AND mask.verified_at IS NOT NULL AND mask.object_version NOT IN('','null')
 AND root.state='ready' AND root.verified_at IS NOT NULL AND root.object_version NOT IN('','null')
 AND (mask.expires_at IS NULL OR mask.expires_at>clock_timestamp())
 AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
 ORDER BY root.id LIMIT 2`, s.Tenant, pixels.ArtifactID, pixels.SHA256,
			source.AssetVersionID, rootSHA, source.Source.ArtifactID, source.Source.SHA256,
			s.Request.ProjectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			count++
			if err := rows.Scan(&found.ArtifactID, &found.SHA256); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 1 || found.SHA256 != rootSHA || !validImportRef(found) {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	return found, ctx.Err()
}

var _ PortablePersonOriginRootDiscoverer = SQLExportDocumentCatalog{}
