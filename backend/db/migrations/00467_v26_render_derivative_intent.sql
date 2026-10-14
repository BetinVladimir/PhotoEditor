-- +goose Up
-- Durable intent reuses tenant-owned runs; dispatch/quota/materialization remain separate.
ALTER TABLE photo.runs ADD COLUMN v26_derivative_task_id uuid;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v26_derivative_source_fk
 FOREIGN KEY(tenant_id,v26_derivative_task_id) REFERENCES photo.tasks(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
CREATE UNIQUE INDEX runs_v26_derivative_source_unique ON photo.runs(tenant_id,v26_derivative_task_id)
 WHERE v26_derivative_task_id IS NOT NULL;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v26_derivative_kind CHECK(v26_derivative_task_id IS NULL OR kind='preview');
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v26_derivative_intent() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v26_derivative_task_id IS NOT NULL AND current_user<>'photo_fn_owner' THEN
   RAISE EXCEPTION 'Derivative intent requires trusted admission' USING ERRCODE='42501';END IF;
 ELSE
  IF NEW.v26_derivative_task_id IS DISTINCT FROM OLD.v26_derivative_task_id OR
   (OLD.v26_derivative_task_id IS NOT NULL AND
    ROW(NEW.id,NEW.tenant_id,NEW.kind,NEW.created_by,NEW.input_snapshot_id,NEW.config_snapshot,NEW.config_hash)
    IS DISTINCT FROM ROW(OLD.id,OLD.tenant_id,OLD.kind,OLD.created_by,OLD.input_snapshot_id,OLD.config_snapshot,OLD.config_hash)) THEN
   RAISE EXCEPTION 'Derivative intent is immutable' USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v26_derivative_intent() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v26_derivative_intent() FROM PUBLIC;
CREATE TRIGGER runs_v26_derivative_intent_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v26_derivative_intent();
-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_v26_render_derivative(p_tenant uuid,p_task uuid,p_result uuid,p_srgb_sha text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source record; parent photo.runs%ROWTYPE; old photo.runs%ROWTYPE; body jsonb; identity uuid; h bytea;
BEGIN
 IF p_srgb_sha IS NULL OR p_srgb_sha !~ '^[0-9a-f]{64}$' OR p_result IS NULL THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO source FROM photo.v26_render_derivative_source(p_tenant,p_task);
 IF NOT FOUND OR source.result_id<>p_result THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=source.project_id AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=source.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source.root_id FOR SHARE;
 SELECT * INTO source FROM photo.v26_render_derivative_source(p_tenant,p_task);
 IF NOT FOUND OR source.result_id<>p_result THEN RETURN NULL;END IF;
 body:=jsonb_build_object('schema_version',1,'project_id',source.project_id,'source_run_id',source.run_id,
  'source_task_id',p_task,'source_result_id',p_result,'source_attempt',source.attempt,'source_fence',source.fence,
  'root',jsonb_build_object('artifact_id',source.root_id,'sha256',source.root_sha),
  'srgb_sha256',p_srgb_sha,'variants',jsonb_build_array('preview1600','preview2560'));
 h:=public.digest(uuid_send(p_task)||convert_to('render-derivative:'||p_result::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 identity:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 SELECT * INTO old FROM photo.runs WHERE tenant_id=p_tenant AND v26_derivative_task_id=p_task FOR UPDATE;
 IF FOUND THEN
  IF old.id=identity AND old.config_snapshot=body AND NOT old.cancel_requested THEN RETURN identity;END IF;
  RETURN NULL;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE id=identity) THEN RETURN NULL;END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,v26_derivative_task_id)
 VALUES(identity,p_tenant,'preview','draft',parent.input_snapshot_id,body,
  encode(sha256(convert_to(body::text,'UTF8')),'hex'),parent.created_by,false,p_task);
 RETURN identity;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enqueue_v26_render_derivative(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_v26_render_derivative(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.enqueue_v26_render_derivative(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
