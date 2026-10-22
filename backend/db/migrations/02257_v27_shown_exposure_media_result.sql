-- +goose Up
-- Canonical media result publication repeats source authority under lifecycle locks.
-- +goose StatementBegin
ALTER FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text)
 RENAME TO commit_task_success_pre_shown_media;
REVOKE ALL ON FUNCTION photo.commit_task_success_pre_shown_media(uuid,integer,bigint,uuid,text)
 FROM PUBLIC,photo_worker,photo_api,photo_gateway,photo_scheduler,photo_portal;
CREATE FUNCTION photo.commit_task_success(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_manifest text)
RETURNS TABLE(outcome text,result_id uuid,result_digest text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node photo.media_retry_nodes%ROWTYPE;v photo.asset_versions%ROWTYPE;source photo.runs%ROWTYPE;
 t photo.tasks%ROWTYPE;committed record;guarded boolean:=false;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 -- Historical successful result replay remains owned by the existing chain.
 IF t.state='running' THEN
  SELECT * INTO node FROM photo.media_retry_nodes WHERE tenant_id=t.tenant_id AND id=t.id;
  IF FOUND THEN
   SELECT * INTO v FROM photo.asset_versions WHERE tenant_id=t.tenant_id AND id=node.asset_version_id;
   SELECT * INTO source FROM photo.runs WHERE tenant_id=t.tenant_id AND id=v.created_by_run_id;
   guarded:=coalesce(source.config_snapshot ? 'shown_semantic',false);
   IF guarded THEN
    PERFORM id FROM photo.tenants WHERE id=t.tenant_id FOR UPDATE;
    PERFORM p.id FROM photo.assets a JOIN photo.projects p ON p.tenant_id=a.tenant_id AND p.id=a.project_id
     WHERE a.tenant_id=t.tenant_id AND a.id=v.asset_id FOR UPDATE OF p;
    PERFORM id FROM photo.assets WHERE tenant_id=t.tenant_id AND id=v.asset_id FOR UPDATE;
    IF photo.semantic_shown_media_task_source_live(p_task) IS DISTINCT FROM true THEN
     RETURN QUERY SELECT 'stale'::text,NULL::uuid,NULL::text;RETURN;
    END IF;
   END IF;
  END IF;
 END IF;
 SELECT * INTO committed FROM photo.commit_task_success_pre_shown_media(p_task,p_attempt,p_fence,p_worker,p_manifest);
 IF guarded AND committed.outcome='committed'
  AND photo.semantic_shown_media_task_source_live(p_task) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'SEMANTIC_SHOWN_MEDIA_RESULT_STALE' USING ERRCODE='23514';
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
