-- +goose Up
-- Exact completed defects.propose provenance for portable DefectLabelMap.
-- V27 native/target handoffs need separate result-catalog proof and remain closed.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_defect_label(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,
 p_labels uuid,p_labels_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_source IS NULL OR p_proposal IS NULL OR p_labels IS NULL
  OR p_source=p_proposal OR p_source=p_labels OR p_proposal=p_labels
  OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_proposal_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_labels_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_source,p_source_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_proposal,p_proposal_sha,'DefectCandidates');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_labels,p_labels_sha,'DefectLabelMap');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.snapshot_items item
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.master_artifact_id=p_source
  AND version.content_hash=p_source_sha AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
  AND asset.project_id=w.project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts source ON source.tenant_id=version.tenant_id AND source.id=p_source
  AND source.sha256=p_source_sha AND source.output_role='working_image'
 JOIN photo.artifacts proposal ON proposal.tenant_id=version.tenant_id
  AND proposal.id=p_proposal AND proposal.sha256=p_proposal_sha
  AND proposal.output_role='defect_candidates'
 JOIN photo.tasks producer ON producer.tenant_id=proposal.tenant_id
  AND producer.id=proposal.created_by_task_id
 JOIN photo.artifacts labels ON labels.tenant_id=producer.tenant_id
  AND labels.id=p_labels AND labels.sha256=p_labels_sha
  AND labels.created_by_task_id=producer.id AND labels.output_role='defect_labels'
 JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id
  AND report.created_by_task_id=producer.id AND report.output_role='defect_propose_report'
 JOIN photo.artifacts protection ON protection.tenant_id=producer.tenant_id
  AND protection.created_by_task_id=producer.id AND protection.output_role='defect_protection'
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id
  AND params.id=producer.params_artifact_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
  AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id
  AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt
  AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
 LEFT JOIN photo.v2_workflows origin_workflow ON origin_workflow.tenant_id=origin.tenant_id
  AND origin_workflow.run_id=origin.id
 WHERE item.tenant_id=p_tenant AND item.snapshot_id=w.input_snapshot_id
  AND item.artifact_id=p_source AND item.sha256=p_source_sha
  AND (origin_workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
  AND NOT origin.cancel_requested AND producer.operation='defects.propose'
  AND producer.operation_version='1' AND producer.resource_class='cpu'
  AND producer.state='succeeded' AND NOT producer.cancel_requested
  AND NOT (origin.config_snapshot ? 'v27_target_defect_prepare'
   OR origin.config_snapshot ? 'v27_defect_native_prepare')
  AND attempt.backend='cpu' AND attempt.backend=producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND producer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha))
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_proposal,'sha256',p_proposal_sha),
   jsonb_build_object('artifact_id',p_labels,'sha256',p_labels_sha),
   jsonb_build_object('artifact_id',report.id,'sha256',report.sha256),
   jsonb_build_object('artifact_id',protection.id,'sha256',protection.sha256))
  AND proposal.produced_attempt=producer.attempt AND proposal.produced_fence=producer.fencing_token
  AND labels.produced_attempt=producer.attempt AND labels.produced_fence=producer.fencing_token
  AND report.produced_attempt=producer.attempt AND report.produced_fence=producer.fencing_token
  AND protection.produced_attempt=producer.attempt AND protection.produced_fence=producer.fencing_token
  AND source.media_type='application/json' AND proposal.media_type='application/json'
  AND report.media_type='application/json' AND params.media_type='application/json'
  AND labels.media_type='application/octet-stream'
  AND protection.media_type='application/octet-stream'
  AND source.bytes BETWEEN 2 AND 1048576
  AND proposal.bytes BETWEEN 2 AND 1048576
  AND report.bytes BETWEEN 2 AND 1048576 AND params.bytes BETWEEN 2 AND 1048576
  AND labels.bytes>=32 AND protection.bytes>=32
  AND NOT EXISTS(SELECT 1 FROM (VALUES
   (source.state,source.verified_at,source.object_version,source.expires_at),
   (proposal.state,proposal.verified_at,proposal.object_version,proposal.expires_at),
   (labels.state,labels.verified_at,labels.object_version,labels.expires_at),
   (report.state,report.verified_at,report.object_version,report.expires_at),
   (protection.state,protection.verified_at,protection.object_version,protection.expires_at),
   (params.state,params.verified_at,params.object_version,params.expires_at))
   checked(state,verified,version,expires)
   WHERE state IS DISTINCT FROM 'ready' OR verified IS NULL OR version IS NULL
    OR version IN('','null') OR expires<=clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_defect_label(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_defect_label(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_defect_label(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
