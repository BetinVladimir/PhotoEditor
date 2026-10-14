-- +goose Up
-- Immutable preview draft only. No quota, task activation or Apply.
ALTER TABLE photo.runs ADD COLUMN v29_multi_quality_task_id uuid;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v29_multi_quality_source_fk
 FOREIGN KEY(tenant_id,v29_multi_quality_task_id) REFERENCES photo.tasks(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v29_multi_quality_kind CHECK(v29_multi_quality_task_id IS NULL OR
 (kind='preview' AND v26_derivative_task_id IS NULL AND v29_perspective_preview_task_id IS NULL AND v29_multi_preview_task_id IS NULL AND v29_perspective_quality_task_id IS NULL));
CREATE UNIQUE INDEX runs_v29_multi_quality_identity ON photo.runs(tenant_id,v29_multi_quality_task_id,
 (config_snapshot->>'source_result_id'),(config_snapshot->>'source_attempt'),(config_snapshot->>'source_fence'))
 WHERE v29_multi_quality_task_id IS NOT NULL;
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_multi_quality_intent() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v29_multi_quality_task_id IS NOT NULL AND current_user<>'photo_fn_owner' THEN
   RAISE EXCEPTION 'Multi quality requires trusted admission' USING ERRCODE='42501';END IF;
 ELSE
  IF NEW.v29_multi_quality_task_id IS DISTINCT FROM OLD.v29_multi_quality_task_id OR
   (OLD.v29_multi_quality_task_id IS NOT NULL AND
    ROW(NEW.id,NEW.tenant_id,NEW.kind,NEW.created_by,NEW.input_snapshot_id,NEW.config_snapshot,NEW.config_hash)
    IS DISTINCT FROM ROW(OLD.id,OLD.tenant_id,OLD.kind,OLD.created_by,OLD.input_snapshot_id,OLD.config_snapshot,OLD.config_hash)) THEN
   RAISE EXCEPTION 'Multi quality intent is immutable' USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_multi_quality_intent() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_multi_quality_intent() FROM PUBLIC;
CREATE TRIGGER runs_v29_multi_quality_intent_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_multi_quality_intent();

CREATE FUNCTION photo.save_v29_multi_quality_intent(p_tenant uuid,p_task uuid,p_operation text,p_result uuid,
 p_attempt integer,p_fence bigint,p_root uuid,p_root_sha text,p_run uuid)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source record; parent photo.runs%ROWTYPE; old photo.runs%ROWTYPE; body jsonb; h bytea; identity uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_task IS NULL OR p_result IS NULL OR p_root IS NULL OR p_run IS NULL
 OR p_operation IS NULL OR p_operation NOT IN('optical.panorama','optical.focus_stack')
 OR p_attempt IS NULL OR p_attempt NOT BETWEEN 1 AND 6 OR p_fence IS NULL OR p_fence<1
 OR coalesce(p_root_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO source FROM photo.v29_multi_preview_source(p_tenant,p_task,p_operation);
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=source.project_id AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=source.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source.root_id FOR SHARE;
 SELECT * INTO source FROM photo.v29_multi_preview_source(p_tenant,p_task,p_operation);
 IF NOT FOUND OR source.result_id IS DISTINCT FROM p_result OR source.attempt IS DISTINCT FROM p_attempt
 OR source.fence IS DISTINCT FROM p_fence OR source.root_id IS DISTINCT FROM p_root OR source.root_sha IS DISTINCT FROM p_root_sha THEN RETURN NULL;END IF;
 h:=public.digest(uuid_send(source.run_id)||convert_to('V29.MultiQuality:'||p_operation||':'||p_task::text||':'||p_result::text||':'||p_root_sha||':'||p_attempt::text||':'||p_fence::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 identity:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF identity<>p_run THEN RETURN NULL;END IF;
 body:=jsonb_build_object('schema_version',1,'project_id',source.project_id,'operation',p_operation,
 'source_run_id',source.run_id,'source_workflow_id',source.workflow_id,'source_task_id',p_task,'source_result_id',p_result,
 'source_attempt',p_attempt,'source_fence',p_fence,'root',jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha),
 'actor_id',source.actor_id,'auth_session_id',source.auth_session_id,'snapshot_id',source.snapshot_id,
 'multi_params_jcs',source.params_jcs,'stage','master','threshold_profile_id','master_v1');
 SELECT * INTO old FROM photo.runs WHERE id=identity FOR UPDATE;
 IF FOUND THEN
  IF old.tenant_id=p_tenant AND old.v29_multi_quality_task_id=p_task AND old.config_snapshot=body AND NOT old.cancel_requested THEN RETURN identity;END IF;
  RETURN NULL;
 END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,v29_multi_quality_task_id)
 VALUES(identity,p_tenant,'preview','draft',parent.input_snapshot_id,body,encode(sha256(convert_to(body::text,'UTF8')),'hex'),source.actor_id,false,p_task);
 RETURN identity;
END $$;
ALTER FUNCTION photo.save_v29_multi_quality_intent(uuid,uuid,text,uuid,integer,bigint,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v29_multi_quality_intent(uuid,uuid,text,uuid,integer,bigint,uuid,text,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.save_v29_multi_quality_intent(uuid,uuid,text,uuid,integer,bigint,uuid,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
