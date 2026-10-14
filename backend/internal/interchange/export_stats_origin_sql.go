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

func (s SQLExportDocumentCatalog) LookupExportStatsOrigin(ctx context.Context, entry v2input.Entry, raw []byte) (ExportStatsOriginEvidence, error) {
	return lookupExportStatsOrigin(ctx, s, s.DB, s.Tenant, s.Who, s.Bucket, entry, raw)
}
func (s SQLExportResultInputCatalog) LookupExportStatsOrigin(ctx context.Context, entry v2input.Entry, raw []byte) (ExportStatsOriginEvidence, error) {
	config := s.Export
	return lookupExportStatsOrigin(ctx, s, config.DB, config.Tenant, config.Who, config.Bucket, entry, raw)
}

type exportStatsOriginCatalog interface {
	ExportStatsAuthority
	ExportDocumentCatalog
}

func lookupExportStatsOrigin(ctx context.Context, catalog exportStatsOriginCatalog, db *pgxpool.Pool, tenantID uuid.UUID, who auth.Identity, bucketName string, entry v2input.Entry, raw []byte) (ExportStatsOriginEvidence, error) {
	var zero, result ExportStatsOriginEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || db == nil || tenantID == uuid.Nil || len(bucketName) < 3 {
		return zero, v2input.ErrInvalid
	}
	// Current accepted source, exact stats object, canonical completed producer
	// and Params readiness are proved before exposing original Params metadata.
	body := append([]byte(nil), raw...)
	if err := catalog.QualifyExportStats(ctx, entry, body); err != nil {
		return zero, err
	}
	err := tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT params.id,params.sha256,params.bucket,params.object_key,params.object_version,params.sha256,params.bytes,params.media_type
   FROM photo.artifacts stats
   JOIN photo.tasks producer ON producer.tenant_id=stats.tenant_id AND producer.id=stats.created_by_task_id
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   WHERE stats.tenant_id=$1 AND stats.id=$2 AND stats.sha256=$3 AND stats.output_role='stats'
   AND producer.operation='color.stats' AND producer.operation_version='1' AND producer.state='succeeded'
   AND stats.produced_attempt=producer.attempt AND stats.produced_fence=producer.fencing_token
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text))
   AND params.state='ready' AND params.verified_at IS NOT NULL AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
   AND params.object_version IS NOT NULL AND params.object_version NOT IN('','null')
   AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())`, tenantID, entry.Ref.ArtifactID, entry.Ref.SHA256).Scan(
			&result.Params.ArtifactID, &result.Params.SHA256, &bucket, &result.Object.Key, &result.Object.VersionID, &result.Object.SHA256, &result.Object.Bytes, &result.Object.ContentType)
		if err != nil {
			return err
		}
		if bucket != bucketName || !validImportRef(result.Params) || result.Object.Key == "" || result.Object.VersionID == "" || result.Object.VersionID == "null" || result.Object.SHA256 != result.Params.SHA256 {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	// Repeat the full request/source/producer proof after metadata access.
	if err := catalog.QualifyExportStats(ctx, entry, body); err != nil {
		return zero, err
	}
	return result, ctx.Err()
}

var _ ExportStatsOriginSource = SQLExportDocumentCatalog{}
var _ ExportStatsOriginSource = SQLExportResultInputCatalog{}
