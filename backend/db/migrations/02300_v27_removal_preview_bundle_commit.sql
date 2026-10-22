-- +goose Up
-- Successful replay belongs to the existing canonical-result chain.
-- Fresh removal publication holds review lifecycle locks through commit.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_removal_preview_bundle_ready(p_task uuid,p_result uuid,p_attempt integer,p_fence bigint)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT COALESCE((SELECT (EXISTS(SELECT 1 FROM photo.task_results z JOIN photo.artifacts a
   ON a.tenant_id=z.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid
   AND a.sha256=z.outputs->0->>'sha256'
   WHERE z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=p_result
   AND z.attempt=p_attempt AND z.fencing_token=p_fence
   AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
   AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
   AND a.output_role='preview_presentation' AND a.media_type='application/json'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 1048576
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
  AND NOT EXISTS(SELECT 1 FROM (VALUES
   ('working_image','application/json',1048576::bigint),
   ('pixels','application/x-npy',2400016384::bigint),
   ('preview_evidence','application/json',1048576::bigint),
   ('before','image/png',33882112::bigint),('after','image/png',33882112::bigint),
   ('affected','image/png',32833536::bigint),('protected','image/png',32833536::bigint)
  ) required(role,media,maximum) WHERE NOT EXISTS(
   SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=t.tenant_id
   AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
   AND a.output_role=required.role AND a.media_type=required.media
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND required.maximum
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))))
 FROM photo.tasks t WHERE t.id=p_task AND t.operation='semantic.preview_remove_object'
 AND t.operation_version='1' AND t.resource_class='gpu'
 AND t.canonical_result_id=p_result AND t.state='succeeded'
 AND t.attempt=p_attempt AND t.fencing_token=p_fence),false);
$$;
ALTER FUNCTION photo.semantic_removal_preview_bundle_ready(uuid,uuid,integer,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_removal_preview_bundle_ready(uuid,uuid,integer,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_semantic_admission;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text)
 RENAME TO commit_task_success_before_removal_bundle;
REVOKE ALL ON FUNCTION photo.commit_task_success_before_removal_bundle(uuid,integer,bigint,uuid,text)
 FROM PUBLIC,photo_worker,photo_api,photo_gateway,photo_scheduler,photo_portal;
CREATE FUNCTION photo.commit_task_success(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_manifest text)
RETURNS TABLE(outcome text,result_id uuid,result_digest text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;e jsonb;config jsonb;committed record;guarded boolean:=false;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.operation='semantic.preview_remove_object' AND t.state='running' THEN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 config:=r.config_snapshot->'semantic_removal_preview';e:=config->'params';
 IF photo.semantic_removal_preview_task_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT 'stale'::text,NULL::uuid,NULL::text;RETURN;END IF;
 PERFORM photo.lock_semantic_decision_publication(t.tenant_id,(e->>'intent_id')::uuid,
 (config->>'actor_id')::uuid,(config->>'session_id')::uuid,(e->>'workflow_revision')::bigint);
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=t.tenant_id
 AND intent_id=(e->>'intent_id')::uuid AND id=(e->>'target_id')::uuid FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id FOR UPDATE;
 IF photo.semantic_removal_preview_task_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT 'stale'::text,NULL::uuid,NULL::text;RETURN;END IF;
 guarded:=true;
 END IF;
 SELECT * INTO committed FROM photo.commit_task_success_before_removal_bundle(p_task,p_attempt,p_fence,p_worker,p_manifest);
 IF guarded AND committed.outcome='committed'
 AND photo.semantic_removal_preview_run_current(t.tenant_id,t.run_id) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_REMOVAL_PREVIEW_RESULT_STALE' USING ERRCODE='23514';END IF;
 -- Validate the committed metadata in the same transaction. An exception rolls
 -- back task success, resource release and outbox publication together.
 IF guarded AND committed.outcome='committed' THEN
  IF photo.semantic_removal_preview_bundle_ready(p_task,committed.result_id,p_attempt,p_fence) IS DISTINCT FROM true THEN
   RAISE EXCEPTION 'SEMANTIC_REMOVAL_PREVIEW_BUNDLE_INCOMPLETE' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN QUERY SELECT committed.outcome,committed.result_id,committed.result_digest;
END $$;
ALTER FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text)
 FROM PUBLIC,photo_api,photo_gateway,photo_scheduler,photo_portal;
GRANT EXECUTE ON FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
