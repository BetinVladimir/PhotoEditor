-- +goose Up
-- Saved immutable input recovery for dedicated assembly adoption, not completion.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_assembly_adoption_inputs(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_params text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;source jsonb;root jsonb;shards jsonb;params_sha text;previous text;ready boolean;
BEGIN
 IF p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 OR p_result IS NULL
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 IF NOT FOUND OR t.operation<>'v2.delivery.assemble' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt
 OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result
 OR NOT EXISTS(SELECT 1 FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task)
 OR NOT EXISTS(SELECT 1 FROM photo.v2_adoption_context(p_tenant,p_task)) THEN RETURN NULL;END IF;
 params_sha:=encode(sha256(convert_to(p_params,'UTF8')),'hex');
 previous:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 BEGIN
  ready:=photo.v30_delivery_assembly_source_ready(p_tenant,t.run_id,t.id,t.params_artifact_id,params_sha,p_params);
  IF ready IS TRUE THEN source:=photo.v30_delivery_assembly_source_context(p_tenant,t.run_id,t.id,t.params_artifact_id,params_sha,p_params);END IF;
 EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('app.tenant_id',coalesce(previous,''),true);RAISE;
 END;
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
 IF ready IS DISTINCT FROM true OR source IS NULL THEN RETURN NULL;END IF;
 root:=(source->>'assembly_jcs')::jsonb;
 SELECT jsonb_agg(b.v30_delivery_document_jcs ORDER BY refs.ordinality) INTO shards
 FROM jsonb_array_elements(root->'shards') WITH ORDINALITY refs(value,ordinality)
 JOIN photo.backend_documents b ON b.tenant_id=p_tenant AND b.run_id=t.run_id AND b.owner_id=t.id
 AND b.artifact_id=(refs.value->>'artifact_id')::uuid AND b.sha256=refs.value->>'sha256' AND b.state='ready';
 IF shards IS NULL OR jsonb_array_length(shards)<>jsonb_array_length(root->'shards') THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('proof',source->'proof','assembly_jcs',source->>'assembly_jcs','shards_jcs',shards,
 'run_id',t.run_id,'package_id',source->>'package_id','params',jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',params_sha));
END $$;
ALTER FUNCTION photo.v30_delivery_assembly_adoption_inputs(uuid,uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_assembly_adoption_inputs(uuid,uuid,integer,bigint,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_assembly_adoption_inputs(uuid,uuid,integer,bigint,uuid,text) TO photo_scheduler;
-- Prevent generic adoption from acknowledging assembly without package lifecycle.
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_v30_assembly;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_v30_assembly(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='v2.delivery.assemble') THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_pre_v30_assembly(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
