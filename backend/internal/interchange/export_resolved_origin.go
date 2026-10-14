package interchange

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Metadata evidence only; full native ResolvedStyle schema, original Params
// content equivalence and current model/consent authority remain mandatory.
type ExportResolvedOriginEvidence struct {
	Params v2input.Ref
	Object objectstore.Object
}

func (s SQLExportDocumentCatalog) LookupExportResolvedOrigin(ctx context.Context, entry v2input.Entry, raw []byte) (ExportResolvedOriginEvidence, error) {
	return lookupExportResolvedOrigin(ctx, s, s.DB, s.Tenant, s.Who, s.Request.ProjectID, s.Bucket, entry, raw)
}
func (s SQLExportResultInputCatalog) LookupExportResolvedOrigin(ctx context.Context, entry v2input.Entry, raw []byte) (ExportResolvedOriginEvidence, error) {
	config := s.Export
	return lookupExportResolvedOrigin(ctx, s, config.DB, config.Tenant, config.Who, config.Request.ProjectID, config.Bucket, entry, raw)
}
func lookupExportResolvedOrigin(ctx context.Context, catalog ExportDocumentCatalog, db *pgxpool.Pool, tenantID uuid.UUID, who auth.Identity, project uuid.UUID, bucketName string, entry v2input.Entry, raw []byte) (ExportResolvedOriginEvidence, error) {
	var zero, out ExportResolvedOriginEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || db == nil || tenantID == uuid.Nil || project == uuid.Nil || len(bucketName) < 3 || entry.Contract != "ResolvedStyle" || !validImportRef(entry.Ref) || rawSHA(raw) != entry.Ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	children, err := exportResolvedChildren(entry, raw)
	if err != nil {
		return zero, err
	}
	var body struct {
		Params struct {
			Image v2input.Ref `json:"image"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &body) != nil || !validImportRef(body.Params.Image) {
		return zero, v2input.ErrInvalid
	}
	// Require all referenced original inputs, including embedded override masks
	// and warp fields. Absolute LUT is a result, not a producer input.
	inputs := []v2input.Ref{}
	for _, child := range children {
		if child.Contract != "AbsoluteLUTArray" {
			inputs = append(inputs, child.Ref)
		}
	}
	inputRaw, err := canonical.Marshal(inputs)
	if err != nil {
		return zero, err
	}
	resolved, err := catalog.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	err = tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT params.id,params.sha256,params.bucket,params.object_key,params.object_version,params.sha256,params.bytes,params.media_type
   FROM photo.artifacts resolved
   JOIN photo.tasks producer ON producer.tenant_id=resolved.tenant_id AND producer.id=resolved.created_by_task_id
   JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id AND workflow.run_id=origin.id
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   WHERE resolved.tenant_id=$1 AND resolved.id=$2 AND resolved.sha256=$3
   AND producer.operation='style.resolve' AND producer.operation_version='1' AND producer.state='succeeded' AND producer.resource_class='cpu'
   AND attempt.finished_at IS NOT NULL AND attempt.backend='cpu' AND producer.execution_profile->>'backend'='cpu' AND attempt.profile->>'backend'='cpu'
   AND NOT origin.cancel_requested AND (workflow.project_id=$4 OR origin.config_snapshot->>'project_id'=$4::text)
   AND photo.v2_editor_authorized($1,$4,$5,$6)
   AND EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
    WHERE v.tenant_id=$1 AND a.project_id=$4 AND v.master_artifact_id=$7 AND v.content_hash=$8 AND v.review_state='accepted' AND a.deleted_at IS NULL AND a.upload_state='verified')
   AND resolved.output_role='resolved_style' AND resolved.produced_attempt=producer.attempt AND resolved.produced_fence=producer.fencing_token
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text))
   AND producer.inputs @> $9::jsonb
   AND resolved.bucket=$10 AND resolved.object_key=$11 AND resolved.object_version=$12 AND resolved.bytes=$13 AND resolved.media_type='application/json'
   AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
   AND NOT EXISTS(SELECT 1 FROM (VALUES(resolved.id),(params.id)) required(id) JOIN photo.artifacts a ON a.tenant_id=$1 AND a.id=required.id
    WHERE a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))`,
			tenantID, entry.Ref.ArtifactID, entry.Ref.SHA256, project, who.UserID, who.SessionID, body.Params.Image.ArtifactID, body.Params.Image.SHA256, inputRaw, bucketName, resolved.Key, resolved.VersionID, resolved.Bytes).Scan(
			&out.Params.ArtifactID, &out.Params.SHA256, &bucket, &out.Object.Key, &out.Object.VersionID, &out.Object.SHA256, &out.Object.Bytes, &out.Object.ContentType)
		if err != nil {
			return err
		}
		if bucket != bucketName || !validImportRef(out.Params) || out.Object.Key == "" || out.Object.SHA256 != out.Params.SHA256 {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	current, err := catalog.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	if current != resolved {
		return zero, v2input.ErrInvalid
	}
	return out, ctx.Err()
}

var _ ExportResolvedOriginSource = SQLExportResultInputCatalog{}
