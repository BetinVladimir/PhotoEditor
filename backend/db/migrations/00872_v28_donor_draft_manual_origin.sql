-- +goose Up
-- Scheduler-only current original producer authority for a blocked donor task.
-- Original pixel bytes and graph sealing remain independent requirements.
-- Exact document bytes and manual replay remain mandatory before publication.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_donor_draft_manual_origin(p_tenant uuid,p_run uuid,p_task uuid,
 p_target_pixel uuid,p_target_sha text,p_guard_pixel uuid,p_guard_sha text,p_vector uuid,p_vector_sha text)
RETURNS TABLE(root_id uuid,root_sha text,params_id uuid,params_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;producer_id uuid;producer_run_id uuid;
BEGIN
 IF photo.v28_donor_transport_scope(p_tenant,p_run,p_task,'v2_input_manifest') IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT v28_donor_descriptor_jcs::jsonb->'params' INTO body FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 -- Resolve a candidate, lock run/task before artifacts, then require the
 -- final lineage query to match that same candidate (no changed-producer fallback).
 SELECT created_by_task_id INTO producer_id FROM photo.artifacts WHERE tenant_id=p_tenant
 AND id=(body->'eyes_mask'->>'artifact_id')::uuid;
 IF producer_id IS NULL THEN RETURN;END IF;
 SELECT run_id INTO producer_run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer_id;
 IF producer_run_id IS NULL THEN RETURN;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=producer_run_id FOR SHARE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer_id AND run_id=producer_run_id FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM task_id FROM photo.interactive_tasks WHERE tenant_id=p_tenant AND task_id=producer_id FOR SHARE;
 PERFORM task_id FROM photo.task_attempts WHERE tenant_id=p_tenant AND task_id=producer_id ORDER BY attempt,fencing_token FOR SHARE;
 PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=producer_id ORDER BY id FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND (created_by_task_id=producer_id
 OR id IN((body->'eyes_mask'->>'artifact_id')::uuid,(body->'protected'->>'artifact_id')::uuid,p_target_pixel,p_guard_pixel,p_vector)
 OR id=(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer_id)) ORDER BY id FOR SHARE;
 RETURN QUERY WITH candidates AS (
 SELECT root.id rid,root.sha256 rsha,original.id pid,original.sha256 psha,count(*) OVER() matches
 FROM photo.v2_workflows workflow
 JOIN photo.snapshots snapshot ON snapshot.tenant_id=workflow.tenant_id AND snapshot.id=workflow.input_snapshot_id
  AND snapshot.expires_at>clock_timestamp()
 JOIN photo.snapshot_items item ON item.tenant_id=workflow.tenant_id AND item.snapshot_id=workflow.input_snapshot_id
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id AND version.id=item.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id AND asset.project_id=workflow.project_id
 JOIN photo.artifacts source ON source.tenant_id=item.tenant_id AND source.id=item.artifact_id AND source.sha256=item.sha256
  AND source.output_role='working_image' AND source.media_type='application/json' AND source.bytes BETWEEN 2 AND 1048576
 JOIN photo.artifacts target ON target.tenant_id=workflow.tenant_id AND target.id=(body->'eyes_mask'->>'artifact_id')::uuid
  AND target.sha256=body->'eyes_mask'->>'sha256' AND target.output_role='mask_target_ref'
 JOIN photo.artifacts guard ON guard.tenant_id=target.tenant_id AND guard.id=(body->'protected'->>'artifact_id')::uuid
  AND guard.sha256=body->'protected'->>'sha256' AND guard.output_role='mask_guard_ref'
 JOIN photo.tasks producer ON producer.tenant_id=target.tenant_id AND producer.id=target.created_by_task_id
  AND producer.id=guard.created_by_task_id
 JOIN photo.runs producer_run ON producer_run.tenant_id=producer.tenant_id AND producer_run.id=producer.run_id
 JOIN photo.interactive_tasks interactive ON interactive.tenant_id=producer.tenant_id AND interactive.task_id=producer.id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id
  AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id
  AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id AND root.created_by_task_id=producer.id AND root.output_role='rasterized_mask'
 JOIN photo.artifacts original ON original.tenant_id=producer.tenant_id AND original.id=producer.params_artifact_id
 JOIN photo.artifacts target_pixel ON target_pixel.tenant_id=producer.tenant_id AND target_pixel.id=p_target_pixel AND target_pixel.sha256=p_target_sha
  AND target_pixel.created_by_task_id=producer.id AND target_pixel.output_role='mask_target'
 JOIN photo.artifacts guard_pixel ON guard_pixel.tenant_id=producer.tenant_id AND guard_pixel.id=p_guard_pixel AND guard_pixel.sha256=p_guard_sha
  AND guard_pixel.created_by_task_id=producer.id AND guard_pixel.output_role='mask_protected'
 WHERE workflow.tenant_id=p_tenant AND workflow.run_id=p_run
  AND item.asset_version_id=(body->'base'->>'asset_version_id')::uuid
  AND item.artifact_id=(body->'base'->'source'->>'artifact_id')::uuid AND item.sha256=body->'base'->'source'->>'sha256'
  AND version.review_state='accepted' AND version.master_artifact_id=item.artifact_id AND version.content_hash=item.sha256
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
  AND producer.id=producer_id AND producer.run_id=producer_run_id
  AND producer.operation='mask.rasterize' AND producer.operation_version='1' AND producer.state='succeeded' AND NOT producer.cancel_requested
  AND producer_run.kind='mask' AND producer_run.graph_sealed AND NOT producer_run.cancel_requested
  AND interactive.source_artifact_id=item.artifact_id
  AND producer_run.config_snapshot->>'asset_version_id'=body->'base'->>'asset_version_id'
  AND producer_run.config_snapshot->>'source_sha256'=item.sha256
  AND target.produced_attempt=producer.attempt AND target.produced_fence=producer.fencing_token
  AND guard.produced_attempt=producer.attempt AND guard.produced_fence=producer.fencing_token
  AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
  AND target_pixel.produced_attempt=producer.attempt AND target_pixel.produced_fence=producer.fencing_token
  AND guard_pixel.produced_attempt=producer.attempt AND guard_pixel.produced_fence=producer.fencing_token
  AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',target.id,'sha256',target.sha256),
   jsonb_build_object('artifact_id',guard.id,'sha256',guard.sha256),jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),
   jsonb_build_object('artifact_id',target_pixel.id,'sha256',target_pixel.sha256),jsonb_build_object('artifact_id',guard_pixel.id,'sha256',guard_pixel.sha256))
  AND NOT EXISTS(SELECT 1 FROM (VALUES(target.id,target.sha256,'MaskRef'),(guard.id,guard.sha256,'MaskRef'),
   (root.id,root.sha256,'RasterizedMask'),(original.id,original.sha256,'Params'),
   (target_pixel.id,target_pixel.sha256,'NpyArray'),(guard_pixel.id,guard_pixel.sha256,'NpyArray'),
   (item.artifact_id,item.sha256,'WorkingImage')) required(id,sha,contract)
   WHERE NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=p_tenant AND input.task_id=p_task
    AND input.artifact_id=required.id AND input.sha256=required.sha AND input.contract=required.contract))
  AND NOT EXISTS(SELECT 1 FROM (VALUES(target.id),(guard.id),(root.id),(original.id),(target_pixel.id),(guard_pixel.id),(item.artifact_id)) required(id)
   JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=required.id
   WHERE a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
    OR a.expires_at<=clock_timestamp())
  AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
  AND original.media_type='application/json' AND original.bytes BETWEEN 2 AND 1048576
  AND target.media_type='application/json' AND target.bytes BETWEEN 2 AND 1048576
  AND guard.media_type='application/json' AND guard.bytes BETWEEN 2 AND 1048576
  AND target_pixel.media_type='application/octet-stream' AND target_pixel.bytes BETWEEN 32 AND 800065548
  AND guard_pixel.media_type='application/octet-stream' AND guard_pixel.bytes BETWEEN 32 AND 800065548
  AND ((p_vector IS NULL AND p_vector_sha IS NULL) OR EXISTS(SELECT 1 FROM photo.artifacts vector
   JOIN photo.v2_task_inputs input ON input.tenant_id=vector.tenant_id AND input.artifact_id=vector.id AND input.sha256=vector.sha256
   WHERE vector.tenant_id=p_tenant AND vector.id=p_vector AND vector.sha256=p_vector_sha AND input.task_id=p_task AND input.contract='ManualMask'
    AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_vector,'sha256',p_vector_sha))
    AND interactive.declared_inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_vector,'sha256',p_vector_sha))
    AND vector.id NOT IN(root.id,original.id,target.id,guard.id,target_pixel.id,guard_pixel.id,item.artifact_id)
    AND vector.state='ready' AND vector.verified_at IS NOT NULL AND vector.object_version IS NOT NULL AND vector.object_version NOT IN('','null')
    AND (vector.expires_at IS NULL OR vector.expires_at>clock_timestamp()) AND vector.media_type='application/json' AND vector.bytes BETWEEN 2 AND 1048576))
 ) SELECT rid,rsha,pid,psha FROM candidates WHERE matches=1;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_donor_draft_manual_origin(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_draft_manual_origin(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_donor_draft_manual_origin(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
