-- +goose Up
-- Successful replay belongs to the existing canonical-result chain.
-- Fresh background_cleanup publication holds review lifecycle locks through commit.
-- +goose StatementBegin
ALTER FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text)
 RENAME TO commit_task_success_before_background_cleanup_preview;
REVOKE ALL ON FUNCTION photo.commit_task_success_before_background_cleanup_preview(uuid,integer,bigint,uuid,text)
 FROM PUBLIC,photo_worker,photo_api,photo_gateway,photo_scheduler,photo_portal;
CREATE FUNCTION photo.commit_task_success(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_manifest text)
RETURNS TABLE(outcome text,result_id uuid,result_digest text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;e jsonb;config jsonb;committed record;guarded boolean:=false;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.operation='semantic.preview_background_cleanup' AND t.state='running' THEN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 config:=r.config_snapshot->'semantic_background_cleanup_preview';e:=config->'params';
 IF photo.semantic_background_cleanup_preview_task_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT 'stale'::text,NULL::uuid,NULL::text;RETURN;END IF;
 PERFORM photo.lock_semantic_decision_publication(t.tenant_id,(e->>'intent_id')::uuid,
 (config->>'actor_id')::uuid,(config->>'session_id')::uuid,(e->>'workflow_revision')::bigint);
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=t.tenant_id
 AND intent_id=(e->>'intent_id')::uuid AND id=(e->>'target_id')::uuid FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id FOR UPDATE;
 IF photo.semantic_background_cleanup_preview_task_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT 'stale'::text,NULL::uuid,NULL::text;RETURN;END IF;
 guarded:=true;
 END IF;
 SELECT * INTO committed FROM photo.commit_task_success_before_background_cleanup_preview(p_task,p_attempt,p_fence,p_worker,p_manifest);
 IF guarded AND committed.outcome='committed'
 AND photo.semantic_background_cleanup_preview_run_current(t.tenant_id,t.run_id) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_REMOVAL_PREVIEW_RESULT_STALE' USING ERRCODE='23514';END IF;
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
