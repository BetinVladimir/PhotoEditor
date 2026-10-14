-- +goose Up
-- Durable preview intent only; quota, task materialization and dispatch follow separately.
-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_semantic_exposure_preview(p_tenant uuid,p_target uuid,p_result uuid,p_srgb_sha text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source record; parent photo.runs%ROWTYPE; old photo.runs%ROWTYPE; body jsonb; identity uuid;
BEGIN
 IF p_result IS NULL OR p_srgb_sha IS NULL OR p_srgb_sha !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO source FROM photo.semantic_exposure_presentation_source(p_tenant,p_target);
 IF NOT FOUND OR source.result_id<>p_result THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=source.project_id AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=source.child_run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=source.task_id FOR UPDATE;
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=p_tenant AND id=p_target FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source.root_artifact_id FOR SHARE;
 SELECT * INTO source FROM photo.semantic_exposure_presentation_source(p_tenant,p_target);
 IF NOT FOUND OR source.result_id<>p_result THEN RETURN NULL;END IF;
 body:=jsonb_build_object('schema_version',1,'project_id',source.project_id,'source_run_id',source.child_run_id,
  'source_task_id',source.task_id,'source_result_id',p_result,'source_attempt',source.attempt,'source_fence',source.fence,
  'root',jsonb_build_object('artifact_id',source.root_artifact_id,'sha256',source.root_sha256),
  'srgb_sha256',p_srgb_sha,'variants',jsonb_build_array('preview1600','preview2560'));
 identity:=photo.semantic_uuid_v5(source.task_id,'semantic-preview:'||p_result::text);
 SELECT * INTO old FROM photo.runs WHERE tenant_id=p_tenant AND v26_derivative_task_id=source.task_id FOR UPDATE;
 IF FOUND THEN
  IF old.id=identity AND old.config_snapshot=body AND NOT old.cancel_requested THEN RETURN identity;END IF;
  RETURN NULL;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE id=identity) THEN RETURN NULL;END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,v26_derivative_task_id)
 VALUES(identity,p_tenant,'preview','draft',parent.input_snapshot_id,body,
  encode(sha256(convert_to(body::text,'UTF8')),'hex'),parent.created_by,false,source.task_id);
 RETURN identity;
END $$;
ALTER FUNCTION photo.enqueue_semantic_exposure_preview(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_semantic_exposure_preview(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.enqueue_semantic_exposure_preview(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
