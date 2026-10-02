-- +goose Up
-- Stage blocked tasks only; proposal/mask authority and activation are pending.
-- +goose StatementBegin
CREATE FUNCTION photo.stage_v25_original_composition_task(p_tenant uuid,p_run uuid,p_task uuid,p_ordinal integer,
 p_params uuid,p_sha text,p_digest text,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; old photo.tasks%ROWTYPE;
 node jsonb; reserved bigint; key text;
BEGIN
 IF p_task IS NULL OR p_params IS NULL OR p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999
  OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_digest,'') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_profile) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR r.status<>'draft'
  OR r.graph_sealed OR r.cancel_requested OR w.workflow_type<>'preview' OR w.renderer_version<>'v2.2' THEN RETURN false;END IF;
 SELECT bytes INTO reserved FROM photo.quota_reservations WHERE tenant_id=p_tenant
  AND holder_type='run' AND holder_id=p_run AND state='reserved'
  AND expires_at>clock_timestamp() FOR SHARE;
 IF reserved IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id FOR SHARE;
 IF NOT photo.v25_layer_original_plan_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,
  (r.config_snapshot->'v25_composition'->>'auth_session_id')::uuid,w.id,r.config_snapshot,reserved) THEN RETURN false;END IF;
 node:=r.config_snapshot->'v25_composition'->'nodes'->p_ordinal;
 IF node->>'task_id' IS DISTINCT FROM p_task::text
  OR node->'params'->>'artifact_id' IS DISTINCT FROM p_params::text
  OR node->'params'->>'sha256' IS DISTINCT FROM p_sha
  OR node->>'input_digest' IS DISTINCT FROM p_digest
  OR node->'profile' IS DISTINCT FROM p_profile THEN RETURN false;END IF;
 key:='layer.compose:'||p_ordinal::text;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF FOUND THEN RETURN old.run_id=p_run AND old.node_key=key AND old.operation='layer.compose'
  AND old.operation_version='1' AND old.resource_class='cpu' AND old.params_artifact_id=p_params
  AND old.input_digest=p_digest AND (old.execution_profile=p_profile OR (photo.v2_input_closure_ready(old.id)
   AND (old.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=p_profile))
  AND old.state='blocked' AND old.attempt=0 AND NOT old.cancel_requested;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,
  inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(p_task,p_tenant,p_run,key,'layer.compose','1','cpu','blocked','[]'::jsonb,p_params,p_digest,p_profile);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_v25_original_composition_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v25_original_composition_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v25_original_composition_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
