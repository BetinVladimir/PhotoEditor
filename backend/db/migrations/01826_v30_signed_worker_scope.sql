-- +goose Up
-- Admit the signed direct root only when the exact Params ref is declared as
-- SignedDeliveryAssemblyEnvelope. Ordinary prepare/assembly semantics remain.
-- +goose StatementBegin
DO $$ DECLARE definition text;
 needle text:=' root_contract:=CASE t.operation WHEN ''v2.delivery.prepare'' THEN ''FrozenDeliveryInput'' ELSE ''DeliveryAssemblyInput'' END;';
 replacement text:=' root_contract:=CASE
  WHEN t.operation=''v2.delivery.prepare'' THEN ''FrozenDeliveryInput''
  WHEN EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract=''SignedDeliveryAssemblyEnvelope''
   AND i.artifact_id=(params->''assembly''->>''artifact_id'')::uuid
   AND i.sha256=params->''assembly''->>''sha256'') THEN ''SignedDeliveryAssemblyEnvelope''
  ELSE ''DeliveryAssemblyInput'' END;';
BEGIN
 SELECT pg_get_functiondef('photo.v30_delivery_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'::regprocedure)
  INTO definition;
 IF definition IS NULL OR position(needle IN definition)=0
  OR position('SignedDeliveryAssemblyEnvelope' IN definition)>0 THEN
  RAISE EXCEPTION 'DELIVERY_SIGNED_LEASE_SHAPE';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd

-- Worker-only current source scope. The two lease probes surround all SQL
-- matrix/receipt reads; no unleased tenant/actor/parent can reach the adapter.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_source_scope(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE previous text;source jsonb;accepted boolean;
BEGIN
 IF p_tenant IS NULL THEN RETURN NULL;END IF;
 previous:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 BEGIN
  IF photo.v30_delivery_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
   p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true THEN
   PERFORM set_config('app.tenant_id',coalesce(previous,''),true);RETURN NULL;END IF;
  accepted:=photo.v30_delivery_signed_current_matrix(p_tenant,p_run,p_task,p_params,p_sha,p_json);
  IF accepted IS DISTINCT FROM true OR photo.v30_delivery_lease_ready(p_tenant,p_run,p_task,
   p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true THEN
   PERFORM set_config('app.tenant_id',coalesce(previous,''),true);RETURN NULL;END IF;
  source:=photo.v30_delivery_signed_runtime_documents(p_tenant,p_run,p_task,p_params,p_sha,p_json);
  IF source IS NULL OR source->>'actor_id' IS NULL OR source->>'parent_task_id' IS NULL
   OR photo.v30_delivery_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
    p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true THEN
   PERFORM set_config('app.tenant_id',coalesce(previous,''),true);RETURN NULL;END IF;
 EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('app.tenant_id',coalesce(previous,''),true);RAISE;
 END;
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
 RETURN jsonb_build_object('tenant_id',p_tenant,'actor_id',source->>'actor_id',
  'parent_id',p_run,'task_id',p_task,'parent_task_id',source->>'parent_task_id');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_source_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_source_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_signed_source_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
