-- +goose Up
-- Preserve the whole claim chain, repeating shown-media source before/after claim.
-- +goose StatementBegin
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) RENAME TO claim_task_pre_shown_media;
REVOKE ALL ON FUNCTION photo.claim_task_pre_shown_media(uuid,integer,uuid,jsonb)
 FROM PUBLIC,photo_worker,photo_api,photo_scheduler,photo_gateway,photo_portal;
CREATE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE claimed record;node photo.media_retry_nodes%ROWTYPE;job photo.media_retry_jobs%ROWTYPE;
 version photo.asset_versions%ROWTYPE;source photo.runs%ROWTYPE;shown boolean:=false;
BEGIN
 SELECT * INTO node FROM photo.media_retry_nodes WHERE id=p_task;
 IF FOUND THEN
  SELECT * INTO job FROM photo.media_retry_jobs WHERE tenant_id=node.tenant_id AND event_id=node.event_id;
  SELECT * INTO version FROM photo.asset_versions WHERE tenant_id=node.tenant_id AND id=node.asset_version_id;
  SELECT * INTO source FROM photo.runs WHERE tenant_id=node.tenant_id AND id=version.created_by_run_id;
  shown:=coalesce(source.config_snapshot ? 'shown_semantic',false);
  IF shown THEN
   -- Acquire lifecycle locks before the task lock in the existing claim chain.
   PERFORM id FROM photo.tenants WHERE id=node.tenant_id FOR UPDATE;
   PERFORM p.id FROM photo.assets a JOIN photo.projects p ON p.tenant_id=a.tenant_id AND p.id=a.project_id
    WHERE a.tenant_id=node.tenant_id AND a.id=version.asset_id FOR UPDATE OF p;
   PERFORM id FROM photo.assets WHERE tenant_id=node.tenant_id AND id=version.asset_id FOR UPDATE;
   IF job.status IS DISTINCT FROM 'running' OR job.run_id IS DISTINCT FROM node.run_id
    OR photo.semantic_shown_media_source_live(node.event_id,node.inputs) IS DISTINCT FROM true
    OR NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=node.tenant_id AND t.id=p_task
     AND t.run_id=node.run_id AND t.operation=node.operation AND t.operation_version='1'
     AND t.operation IN('media.thumbnail','media.preview') AND t.inputs=node.inputs) THEN
    RAISE EXCEPTION 'SEMANTIC_SHOWN_MEDIA_SOURCE_STALE' USING ERRCODE='23514';
   END IF;
  END IF;
 END IF;
 SELECT * INTO claimed FROM photo.claim_task_pre_shown_media(p_task,p_attempt,p_worker,p_command);
 IF shown AND claimed.outcome='claimed'
  AND photo.semantic_shown_media_source_live(node.event_id,node.inputs) IS DISTINCT FROM true THEN
  -- Exception rolls back claim, lease and capacity mutations together.
  RAISE EXCEPTION 'SEMANTIC_SHOWN_MEDIA_SOURCE_STALE' USING ERRCODE='23514';
 END IF;
 RETURN QUERY SELECT claimed.outcome,claimed.fence,claimed.lease_until,claimed.deadline,claimed.context;
END $$;
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
