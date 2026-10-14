-- +goose Up
-- Persist the exact four-sided preview intent before external Params IO.
-- This function never reserves capacity or activates the draft.
-- +goose StatementBegin
CREATE FUNCTION photo.save_v28_restoration_preview_intent(
 p_tenant uuid,p_task uuid,p_review uuid,p_result uuid,p_attempt integer,
 p_fence bigint,p_root uuid,p_root_sha text,p_srgb text,p_run uuid)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source record; parent photo.runs%ROWTYPE; old photo.runs%ROWTYPE;
 body jsonb; h bytea; identity uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_task IS NULL OR p_review IS NULL OR p_result IS NULL OR p_root IS NULL OR p_run IS NULL
 OR p_attempt IS NULL OR p_attempt NOT BETWEEN 1 AND 6 OR p_fence IS NULL OR p_fence<1
 OR coalesce(p_root_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_srgb,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO source FROM photo.v28_restoration_preview_source(p_tenant,p_task);
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=source.project_id
  AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=source.run_id FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source.root_id FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source.source_id FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source.candidate_id FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO source FROM photo.v28_restoration_preview_source(p_tenant,p_task);
 IF NOT FOUND OR source.review_id IS DISTINCT FROM p_review
 OR source.result_id IS DISTINCT FROM p_result
 OR source.attempt IS DISTINCT FROM p_attempt OR source.fence IS DISTINCT FROM p_fence
 OR source.root_id IS DISTINCT FROM p_root OR source.root_sha IS DISTINCT FROM p_root_sha
 OR parent.id IS DISTINCT FROM source.run_id THEN RETURN NULL;END IF;
 h:=public.digest(uuid_send(source.run_id)||convert_to(
  'V28.RestorationPreview:'||p_task::text||':'||p_result::text||':'||p_root_sha||':'||p_review::text||':'||p_srgb,
  'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 identity:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF identity<>p_run THEN RETURN NULL;END IF;
 body:=jsonb_build_object('schema_version',1,'project_id',source.project_id,
  'source_run_id',source.run_id,'source_workflow_id',source.workflow_id,
  'source_task_id',p_task,'source_review_id',p_review,'source_result_id',p_result,
  'source_attempt',p_attempt,'source_fence',p_fence,
  'root',jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha),
  'before',jsonb_build_object('artifact_id',source.source_id,'sha256',source.source_sha),
  'after',jsonb_build_object('artifact_id',source.candidate_id,'sha256',source.candidate_sha),
  'actor_id',source.actor_id,'auth_session_id',source.auth_session_id,
  'source_version_id',source.source_version_id,'canvas_hash',source.canvas_hash,
  'srgb_sha256',p_srgb,'sides',jsonb_build_array('before','after'),
  'variants',jsonb_build_array('preview1600','preview2560'));
 SELECT * INTO old FROM photo.runs WHERE id=identity FOR UPDATE;
 IF FOUND THEN
  IF old.tenant_id=p_tenant AND old.v28_restoration_preview_task_id=p_task
   AND old.config_snapshot=body AND NOT old.cancel_requested THEN RETURN identity;END IF;
  RETURN NULL;
 END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,
  config_hash,created_by,graph_sealed,v28_restoration_preview_task_id)
 VALUES(identity,p_tenant,'preview','draft',parent.input_snapshot_id,body,
  encode(sha256(convert_to(body::text,'UTF8')),'hex'),source.actor_id,false,p_task);
 RETURN identity;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.save_v28_restoration_preview_intent(uuid,uuid,uuid,uuid,integer,bigint,uuid,text,text,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v28_restoration_preview_intent(uuid,uuid,uuid,uuid,integer,bigint,uuid,text,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.save_v28_restoration_preview_intent(uuid,uuid,uuid,uuid,integer,bigint,uuid,text,text,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
