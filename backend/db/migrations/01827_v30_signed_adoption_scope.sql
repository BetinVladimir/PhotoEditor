-- +goose Up
-- Scheduler-only metadata scope for reconstruction of a succeeded signed
-- Delivery task. Physical V31 bytes and the final manifest commit require
-- independent verification; this function alone cannot adopt a result.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_adoption_scope(
 p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_params text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;source jsonb;previous text;digest text;accepted boolean;
BEGIN
 IF p_tenant IS NULL OR p_task IS NULL OR p_result IS NULL OR p_attempt IS NULL
  OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 OR p_params IS NULL
  OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 IF NOT FOUND OR t.operation<>'v2.delivery.assemble' OR t.operation_version<>'1'
  OR t.resource_class<>'cpu' OR t.state<>'succeeded' OR t.cancel_requested
  OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
  OR t.canonical_result_id IS DISTINCT FROM p_result
  OR NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant
   AND z.id=p_result AND z.task_id=p_task AND z.attempt=p_attempt AND z.fencing_token=p_fence)
  OR NOT EXISTS(SELECT 1 FROM photo.v2_adoption_context(p_tenant,p_task))
  THEN RETURN NULL;END IF;
 digest:=encode(sha256(convert_to(p_params,'UTF8')),'hex');
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
  AND i.task_id=p_task AND i.artifact_id=t.params_artifact_id AND i.sha256=digest
  AND i.contract='Params') THEN RETURN NULL;END IF;
 previous:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 BEGIN
  accepted:=photo.v30_delivery_signed_current_matrix(p_tenant,t.run_id,t.id,
   t.params_artifact_id,digest,p_params);
  IF accepted IS TRUE THEN
   source:=photo.v30_delivery_signed_runtime_documents(p_tenant,t.run_id,t.id,
    t.params_artifact_id,digest,p_params);
  END IF;
 EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('app.tenant_id',coalesce(previous,''),true);RAISE;
 END;
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
 IF accepted IS DISTINCT FROM true OR source IS NULL
  OR source->>'actor_id' IS NULL OR source->>'package_id' IS NULL THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('tenant_id',p_tenant,'actor_id',source->>'actor_id',
  'parent_id',t.run_id,'task_id',p_task,'result_id',p_result,
  'params_artifact_id',t.params_artifact_id,'params_sha',digest,
  'package_id',source->>'package_id');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_adoption_scope(uuid,uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_adoption_scope(uuid,uuid,integer,bigint,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_signed_adoption_scope(uuid,uuid,integer,bigint,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
