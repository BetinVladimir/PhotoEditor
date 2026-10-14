package interchange

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type ExportBodyPersonOriginSource interface {
	LookupExportBodyPersonOrigin(context.Context, v2input.Entry, []byte, ExportClosureNode) (ExportStatsPersonOrigin, error)
}

func (s SQLExportDocumentCatalog) LookupExportBodyPersonOrigin(ctx context.Context, parent v2input.Entry, raw []byte, image ExportClosureNode) (ExportStatsPersonOrigin, error) {
	return lookupExportBodyPersonOrigin(ctx, s, s, parent, raw, image)
}
func (s SQLExportResultInputCatalog) LookupExportBodyPersonOrigin(ctx context.Context, parent v2input.Entry, raw []byte, image ExportClosureNode) (ExportStatsPersonOrigin, error) {
	config, err := exportPolicyCatalogConfig(s)
	if err != nil {
		return ExportStatsPersonOrigin{}, err
	}
	return lookupExportBodyPersonOrigin(ctx, s, config, parent, raw, image)
}
func lookupExportBodyPersonOrigin(ctx context.Context, source ExportParamsOperationSource, s SQLExportDocumentCatalog, parent v2input.Entry, raw []byte, image ExportClosureNode) (ExportStatsPersonOrigin, error) {
	var zero, out ExportStatsPersonOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if source == nil || s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	projected, err := exportBodyPersonProjection(parent, raw, image)
	if err != nil {
		return zero, err
	}
	body := append([]byte(nil), raw...)
	mask := projected.Person
	consumer, err := source.LookupExportParamsOperation(ctx, parent, body)
	if err != nil {
		return zero, err
	}
	err = tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT root.id,root.sha256,original.id,original.sha256,report.id,report.sha256,
   mask.bucket,mask.object_key,mask.object_version,mask.sha256,mask.bytes,mask.media_type
   FROM photo.artifacts mask
   JOIN photo.tasks producer ON producer.tenant_id=mask.tenant_id AND producer.id=mask.created_by_task_id
   JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id AND root.created_by_task_id=producer.id AND root.output_role='person_mask_ref'
   JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.created_by_task_id=producer.id AND report.output_role='person_report'
   JOIN photo.artifacts original ON original.tenant_id=producer.tenant_id AND original.id=producer.params_artifact_id
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   WHERE mask.tenant_id=$1 AND mask.id=$2 AND mask.sha256=$3
   AND producer.operation='mask.person' AND producer.operation_version='1' AND producer.state='succeeded'
   AND mask.output_role='person_mask' AND mask.media_type IN('application/octet-stream','application/x-npy') AND mask.bytes>0
   AND mask.produced_attempt=producer.attempt AND mask.produced_fence=producer.fencing_token
   AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
   AND report.produced_attempt=producer.attempt AND report.produced_fence=producer.fencing_token
   AND (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=$1 AND a.created_by_task_id=producer.id AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token AND a.output_role='person_mask_ref')=1
   AND (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=$1 AND a.created_by_task_id=producer.id AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token AND a.output_role='person_report')=1
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',mask.id,'sha256',mask.sha256),jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),jsonb_build_object('artifact_id',report.id,'sha256',report.sha256))
   AND EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
    WHERE v.tenant_id=$1 AND a.project_id=$4 AND v.master_artifact_id=$5 AND v.content_hash=$6 AND v.review_state='accepted' AND a.deleted_at IS NULL AND a.upload_state='verified'
    AND photo.semantic_proposal_person_mask_origin($1,$4,$7,$8,v.id,$5,$6,root.id,root.sha256,mask.id,mask.sha256))
   AND NOT EXISTS(SELECT 1 FROM (VALUES(mask.id),(root.id),(report.id),(original.id)) required(id) JOIN photo.artifacts a ON a.tenant_id=$1 AND a.id=required.id
    WHERE a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))
   AND mask.bucket=$9 AND root.bucket=$9 AND report.bucket=$9 AND original.bucket=$9
   AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
   AND original.media_type='application/json' AND original.bytes BETWEEN 2 AND 65536
   AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608`, s.Tenant, mask.Artifact.ID, mask.Artifact.SHA256, s.Request.ProjectID, projected.Image.ID, projected.Image.SHA256, s.Who.UserID, s.Who.SessionID, s.Bucket).Scan(&out.Root.ArtifactID, &out.Root.SHA256, &out.Params.ArtifactID, &out.Params.SHA256, &out.Report.ArtifactID, &out.Report.SHA256, &bucket, &out.Mask.Key, &out.Mask.VersionID, &out.Mask.SHA256, &out.Mask.Bytes, &out.Mask.ContentType)
		if err != nil {
			return err
		}
		if bucket != s.Bucket || !validImportRef(out.Root) || !validImportRef(out.Params) || !validImportRef(out.Report) || out.Mask.Key == "" || out.Mask.SHA256 != mask.Artifact.SHA256 {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	final, err := source.LookupExportParamsOperation(ctx, parent, body)
	if err != nil {
		return zero, err
	}
	if consumer != final {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return out, nil
}

var _ ExportBodyPersonOriginSource = SQLExportDocumentCatalog{}
var _ ExportBodyPersonOriginSource = SQLExportResultInputCatalog{}

func exportBodyPersonProjection(parent v2input.Entry, raw []byte, image ExportClosureNode) (review.StatsParams, error) {
	var zero review.StatsParams
	if len(raw) < 2 || len(raw) > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	operation, err := exportParamsOperationShape(raw)
	if err != nil || operation != "geometry.body" {
		return zero, v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	masks, err := exportParamsInlineMasks(parent, body)
	if err != nil || len(masks) != 1 {
		return zero, v2input.ErrInvalid
	}
	mask, err := review.ParseMaskRef(masks[0])
	if err != nil {
		return zero, err
	}
	var params struct {
		Image v2input.Ref `json:"image"`
	}
	if json.Unmarshal(body, &params) != nil || image.Entry != (v2input.Entry{Ref: params.Image, Contract: "WorkingImage"}) || rawSHA(image.Body) != params.Image.SHA256 {
		return zero, v2input.ErrInvalid
	}
	working, err := review.ParseWorkingImage(image.Body)
	if err != nil {
		return zero, err
	}
	// Use the existing complete StatsParams source geometry contract only as a
	// typed image/person projection; consumer authority remains geometry.body.
	projected := review.StatsParams{Image: review.ArtifactRef{ID: params.Image.ArtifactID, SHA256: params.Image.SHA256}, Person: mask}
	if err := projected.ValidateSource(working, projected.Image); err != nil {
		return zero, err
	}
	return projected, nil
}
