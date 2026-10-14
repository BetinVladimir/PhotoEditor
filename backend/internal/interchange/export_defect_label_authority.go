package interchange

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

func (s SQLExportDocumentCatalog) QualifyExportDefectLabel(ctx context.Context, parent v2input.Entry, raw []byte, array v2input.Entry) error {
	return qualifyExportDefectLabel(ctx, s, s, parent, raw, array)
}
func (s SQLExportResultInputCatalog) QualifyExportDefectLabel(ctx context.Context, parent v2input.Entry, raw []byte, array v2input.Entry) error {
	config, err := exportPolicyCatalogConfig(s)
	if err != nil {
		return err
	}
	return qualifyExportDefectLabel(ctx, s, config, parent, raw, array)
}
func qualifyExportDefectLabel(ctx context.Context, documents ExportDocumentCatalog, s SQLExportDocumentCatalog, parent v2input.Entry, raw []byte, array v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if documents == nil || s.DB == nil {
		return v2input.ErrInvalid
	}
	if parent.Contract != "DefectCandidates" || array.Contract != "DefectLabelMap" || !validImportRef(parent.Ref) || !validImportRef(array.Ref) || len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != parent.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	var body struct {
		Image  v2input.Ref `json:"image"`
		Labels v2input.Ref `json:"label_map"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Labels != array.Ref || !validImportRef(body.Image) {
		return v2input.ErrInvalid
	}
	parentObject, err := documents.LookupExportDocument(ctx, parent)
	if err != nil {
		return err
	}
	object, err := documents.LookupExportDocument(ctx, array)
	if err != nil {
		return err
	}
	if !exportArrayMediaType(object.ContentType) {
		return v2input.ErrInvalid
	}
	err = tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(
   SELECT 1 FROM photo.asset_versions v
   JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   JOIN photo.artifacts proposal ON proposal.tenant_id=v.tenant_id AND proposal.id=$5 AND proposal.sha256=$6
   JOIN photo.tasks producer ON producer.tenant_id=proposal.tenant_id AND producer.id=proposal.created_by_task_id
   JOIN photo.runs origin_run ON origin_run.tenant_id=producer.tenant_id AND origin_run.id=producer.run_id
   JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.created_by_task_id=producer.id AND report.output_role='defect_propose_report'
   JOIN photo.artifacts protection ON protection.tenant_id=producer.tenant_id AND protection.created_by_task_id=producer.id AND protection.output_role='defect_protection'
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   JOIN photo.artifacts labels ON labels.tenant_id=producer.tenant_id AND labels.id=$7 AND labels.sha256=$8
   WHERE v.tenant_id=$1 AND a.project_id=$2 AND v.master_artifact_id=$3 AND v.content_hash=$4
   AND v.review_state='accepted' AND a.deleted_at IS NULL AND a.upload_state='verified'
   AND labels.bucket=$11 AND labels.object_key=$12 AND labels.object_version=$13 AND labels.bytes=$14 AND labels.media_type=$15
   AND photo.semantic_proposal_defect_origin($1,$2,$9,$10,v.id,$3,$4,$5,$6,report.id,report.sha256,$7,$8,protection.id,protection.sha256,params.id,params.sha256)
   -- Native preparation must satisfy its dependency catalogue as well as the
   -- completed producer/output proof. Never fall back after native refusal.
   AND NOT (origin_run.config_snapshot ? 'v27_target_defect_prepare' AND origin_run.config_snapshot ? 'v27_defect_native_prepare')
   AND CASE WHEN origin_run.config_snapshot ? 'v27_defect_native_prepare' THEN EXISTS(
    SELECT 1 FROM photo.semantic_native_defect_result_catalog($1,$2,$9,$10,
     (origin_run.config_snapshot->'v27_defect_native_prepare'->>'parent_run_id')::uuid,
     origin_run.config_snapshot->'v27_defect_native_prepare'->>'request_sha',producer.id) native
    WHERE native.result_id=producer.canonical_result_id
    AND native.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$5::uuid,'sha256',$6::text),
     jsonb_build_object('artifact_id',$7::uuid,'sha256',$8::text),
     jsonb_build_object('artifact_id',report.id,'sha256',report.sha256),
     jsonb_build_object('artifact_id',protection.id,'sha256',protection.sha256))
    AND native.params=jsonb_build_object('artifact_id',params.id,'sha256',params.sha256)
    AND origin_run.config_snapshot->'v27_defect_native_prepare'->'source'->>'asset_version_id'=v.id::text
    AND origin_run.config_snapshot->'v27_defect_native_prepare'->'source'->'source'=jsonb_build_object('artifact_id',$3::uuid,'sha256',$4::text)
   )
   WHEN origin_run.config_snapshot ? 'v27_target_defect_prepare' THEN EXISTS(
    SELECT 1 FROM photo.semantic_target_defect_result_catalog($1,$2,$9,$10,
     (origin_run.config_snapshot->'v27_target_defect_prepare'->>'parent_run_id')::uuid,
     origin_run.config_snapshot->'v27_target_defect_prepare'->>'request_sha',producer.id) native
    WHERE native.result_id=producer.canonical_result_id
    AND native.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$5::uuid,'sha256',$6::text),
     jsonb_build_object('artifact_id',$7::uuid,'sha256',$8::text),
     jsonb_build_object('artifact_id',report.id,'sha256',report.sha256),
     jsonb_build_object('artifact_id',protection.id,'sha256',protection.sha256))
    AND native.params=jsonb_build_object('artifact_id',params.id,'sha256',params.sha256)
    AND origin_run.config_snapshot->'v27_target_defect_prepare'->'source'->>'asset_version_id'=v.id::text
    AND origin_run.config_snapshot->'v27_target_defect_prepare'->'source'->'source'=jsonb_build_object('artifact_id',$3::uuid,'sha256',$4::text)
   ) ELSE true END
  )`, s.Tenant, s.Request.ProjectID, body.Image.ArtifactID, body.Image.SHA256, parent.Ref.ArtifactID, parent.Ref.SHA256,
			array.Ref.ArtifactID, array.Ref.SHA256, s.Who.UserID, s.Who.SessionID, s.Bucket, object.Key, object.VersionID, object.Bytes, object.ContentType).Scan(&allowed)
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
	if currentArray != object {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportDefectLabelOccurrenceAuthority = SQLExportDocumentCatalog{}
var _ ExportDefectLabelOccurrenceAuthority = SQLExportResultInputCatalog{}
