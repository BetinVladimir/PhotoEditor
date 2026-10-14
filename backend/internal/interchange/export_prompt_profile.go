package interchange

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Segmentation does not itself require identity/learning consent. If its
// workflow consumed grants, every recorded dependency must still be current.
const exportPromptWorkflowConsentSQL = `NOT EXISTS(
 SELECT 1 FROM photo.workflow_consent_links consent_link
 LEFT JOIN photo.consent_grants consent_grant
  ON consent_grant.tenant_id=consent_link.tenant_id
  AND consent_grant.project_id=consent_link.project_id
  AND consent_grant.id=consent_link.consent_id
 WHERE consent_link.tenant_id=producer.tenant_id AND consent_link.workflow_id=workflow.id
 AND (consent_link.project_id IS DISTINCT FROM workflow.project_id
  OR consent_grant.id IS NULL OR consent_grant.status IS DISTINCT FROM 'granted'
  OR consent_grant.epoch IS DISTINCT FROM consent_link.consent_epoch))`

// Locks stored at task admission and execution must match current verified
// qualification. Dataset/check evidence is verified in the qualification report.
func (s SQLExportDocumentCatalog) QualifyExportPromptProfile(ctx context.Context, origin ExportSeriesMaskOrigin, binding v2input.SessionVersionBinding, expected v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || origin.Operation != "mask.prompt" || origin.Version != "1" || expected.Operation != "mask.prompt" || expected.Version != "1" || !validImportRef(origin.Root.Ref) || !validImportRef(origin.Params.Ref) || !validImportRef(origin.Report.Ref) || !validImportRef(binding.Source) || origin.Attempt < 1 || origin.Fence < 1 {
		return v2input.ErrInvalid
	}
	policy, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	return tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.tasks producer
   JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id AND root.created_by_task_id=producer.id AND root.id=$5 AND root.sha256=$6
   JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.created_by_task_id=producer.id AND report.id=$7 AND report.sha256=$8
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   JOIN photo.runs run ON run.tenant_id=producer.tenant_id AND run.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run.tenant_id AND workflow.run_id=run.id
   WHERE producer.tenant_id=$1 AND producer.id=$2 AND producer.attempt=$3 AND producer.fencing_token=$4
   AND producer.operation='mask.prompt' AND producer.operation_version='1' AND producer.state='succeeded' AND producer.resource_class='gpu'
   AND root.output_role='prompt_mask_ref' AND report.output_role='prompt_mask_report'
   AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token AND report.produced_attempt=producer.attempt AND report.produced_fence=producer.fencing_token
   AND params.id=$9 AND params.sha256=$10 AND params.bucket=$11 AND params.object_key=$12 AND params.object_version=$13 AND params.bytes=$14
   AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$15::uuid,'sha256',$16::text))
   AND attempt.finished_at IS NOT NULL AND attempt.backend=$17::jsonb->>'Backend' AND producer.execution_profile->>'backend'=$17::jsonb->>'Backend' AND attempt.profile->>'backend'=$17::jsonb->>'Backend'
   AND producer.execution_profile->>'model_lock_sha'=$17::jsonb->>'ModelLockSHA' AND attempt.profile->>'model_lock_sha'=$17::jsonb->>'ModelLockSHA'
   AND producer.execution_profile->>'runtime_lock_sha'=$17::jsonb->>'RuntimeLockSHA' AND attempt.profile->>'runtime_lock_sha'=$17::jsonb->>'RuntimeLockSHA'
   AND producer.execution_profile->>'device_profile_sha'=$17::jsonb->>'DeviceProfileSHA' AND attempt.profile->>'device_profile_sha'=$17::jsonb->>'DeviceProfileSHA'
   AND NOT run.cancel_requested AND (workflow.project_id=$18 OR run.config_snapshot->>'project_id'=$18::text)
   AND photo.v2_editor_authorized($1,$18,$19,$20)
   AND `+exportPromptWorkflowConsentSQL+`
   AND EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
    WHERE v.tenant_id=$1 AND v.id=$21 AND a.project_id=$18 AND v.master_artifact_id=$15 AND v.content_hash=$16 AND v.review_state='accepted' AND a.deleted_at IS NULL AND a.upload_state='verified'))`, s.Tenant, origin.Task, origin.Attempt, origin.Fence, origin.Root.Ref.ArtifactID, origin.Root.Ref.SHA256, origin.Report.Ref.ArtifactID, origin.Report.Ref.SHA256, origin.Params.Ref.ArtifactID, origin.Params.Ref.SHA256, s.Bucket, origin.Params.Object.Key, origin.Params.Object.VersionID, origin.Params.Object.Bytes, binding.Source.ArtifactID, binding.Source.SHA256, policy, s.Request.ProjectID, s.Who.UserID, s.Who.SessionID, binding.AssetVersionID).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return v2qualification.ErrUnqualified
		}
		return ctx.Err()
	})
}
func (s SQLExportResultInputCatalog) QualifyExportPromptProfile(ctx context.Context, origin ExportSeriesMaskOrigin, binding v2input.SessionVersionBinding, expected v2qualification.Expected) error {
	config, err := exportPolicyCatalogConfig(s)
	if err != nil {
		return err
	}
	return config.QualifyExportPromptProfile(ctx, origin, binding, expected)
}
