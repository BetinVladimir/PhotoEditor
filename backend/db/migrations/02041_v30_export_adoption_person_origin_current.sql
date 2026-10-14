-- +goose Up
-- Recheck the signed person-origin claim against the live canonical producer.
-- No user session is borrowed; the exact export lease supplies the scope.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_person_origin_current(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_version uuid,p_source uuid,p_source_sha text,p_root uuid,p_root_sha text,
 p_mask uuid,p_mask_sha text,p_producer uuid,p_attempt integer,p_fence bigint,
 p_report uuid,p_report_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project_id uuid;
BEGIN
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT w.project_id INTO project_id FROM photo.v2_workflows w
 WHERE w.tenant_id=p_tenant AND w.id=p_workflow AND w.run_id=p_run FOR SHARE;
 IF project_id IS NULL OR p_producer IS NULL OR p_attempt<1 OR p_fence<1
 OR p_report IS NULL OR coalesce(p_report_sha,'') !~ '^[0-9a-f]{64}$'
 OR photo.semantic_person_mask_producer_ready(p_tenant,project_id,p_source,p_source_sha,
  p_root,p_root_sha,p_mask,p_mask_sha) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.asset_versions version
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 JOIN photo.artifacts mask ON mask.tenant_id=version.tenant_id AND mask.id=p_mask AND mask.sha256=p_mask_sha
 JOIN photo.tasks producer ON producer.tenant_id=mask.tenant_id AND producer.id=mask.created_by_task_id
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id
  AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id
  AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id AND root.created_by_task_id=producer.id
  AND root.id=p_root AND root.sha256=p_root_sha AND root.output_role='person_mask_ref'
 JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.created_by_task_id=producer.id
  AND report.id=p_report AND report.sha256=p_report_sha AND report.output_role='person_report'
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
 WHERE version.tenant_id=p_tenant AND version.id=p_version AND version.review_state='accepted'
  AND version.master_artifact_id=p_source AND version.content_hash=p_source_sha
  AND asset.project_id=project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
  AND producer.id=p_producer AND producer.attempt=p_attempt AND producer.fencing_token=p_fence
  AND producer.operation='mask.person' AND producer.operation_version='1' AND producer.state='succeeded'
  AND attempt.finished_at IS NOT NULL AND attempt.backend=producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND mask.produced_attempt=p_attempt AND mask.produced_fence=p_fence
  AND root.produced_attempt=p_attempt AND root.produced_fence=p_fence
  AND report.produced_attempt=p_attempt AND report.produced_fence=p_fence
  AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
  AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 65536
  AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
  AND mask.media_type IN('application/octet-stream','application/x-npy') AND mask.bytes>0
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',mask.id,'sha256',mask.sha256),
   jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),
   jsonb_build_object('artifact_id',report.id,'sha256',report.sha256))
  AND (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.created_by_task_id=p_producer AND a.output_role='person_mask_ref'
   AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence)=1
  AND (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.created_by_task_id=p_producer AND a.output_role='person_report'
   AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence)=1
  AND NOT EXISTS(SELECT 1 FROM (VALUES(mask.id),(root.id),(report.id),(params.id)) required(id)
   JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=required.id
   WHERE a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL
    OR a.object_version IS NULL OR a.object_version IN('','null')
    OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_person_origin_current(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,integer,bigint,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_person_origin_current(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,integer,bigint,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_person_origin_current(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,integer,bigint,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
