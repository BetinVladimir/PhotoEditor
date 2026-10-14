-- +goose Up
-- Live lease/typed closure gate only. Frozen package/source authority is a
-- separate mandatory admission proof; this function must never replace it.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_lease_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;s photo.resource_slots%ROWTYPE;
 params jsonb;root jsonb;root_contract text;root_key text;
BEGIN
 IF p_json IS NULL OR octet_length(p_json) NOT BETWEEN 2 AND 65536
  OR p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_digest IS NULL OR p_digest !~ '^[0-9a-f]{64}$'
  OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.worker_registrations WHERE tenant_id=p_tenant
  AND worker_id=p_worker AND db_principal=session_user AND enabled) THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.graph_sealed IS DISTINCT FROM true
  OR r.cancel_requested IS DISTINCT FROM false OR r.status NOT IN('queued','running')
  OR t.id IS NULL OR t.operation NOT IN('v2.delivery.prepare','v2.delivery.assemble')
  OR t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
  OR t.state IS DISTINCT FROM 'running' OR t.cancel_requested IS DISTINCT FROM false
  OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
  OR t.worker_id IS DISTINCT FROM p_worker OR t.params_artifact_id IS DISTINCT FROM p_params
  OR t.input_digest IS DISTINCT FROM p_digest
  OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
  OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
  OR t.deadline IS NULL OR t.deadline<=clock_timestamp() THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF s.id IS NULL OR s.fencing_token IS DISTINCT FROM p_fence OR s.resource_class IS DISTINCT FROM 'cpu'
  OR s.lease_until IS NULL OR s.lease_until<=clock_timestamp()
  OR photo.worker_authorized(p_tenant,p_worker,'cpu',s.node_id,t.operation,
   t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') IS DISTINCT FROM true
  OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run
  AND w.renderer_version='v2.2') THEN RETURN false;END IF;
 params:=p_json::jsonb;
 root_key:=CASE t.operation WHEN 'v2.delivery.prepare' THEN 'input' ELSE 'assembly' END;
 root_contract:=CASE t.operation WHEN 'v2.delivery.prepare' THEN 'FrozenDeliveryInput' ELSE 'DeliveryAssemblyInput' END;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR params->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR (SELECT count(*) FROM jsonb_object_keys(params))<>2 OR NOT params ? root_key THEN RETURN false;END IF;
 root:=params->root_key;
 IF jsonb_typeof(root) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(root))<>2
  OR coalesce(root->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR root->>'artifact_id'='00000000-0000-0000-0000-000000000000'
  OR coalesce(root->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run
  AND d.owner_id=p_task AND d.purpose='task_params' AND d.state='ready'
  AND d.artifact_id=p_params AND d.sha256=p_sha AND d.bytes=octet_length(p_json)) THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract=root_contract)<>1
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.contract=root_contract AND i.artifact_id=(root->>'artifact_id')::uuid AND i.sha256=root->>'sha256')
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.contract='Params' AND i.artifact_id=p_params AND i.sha256=p_sha) THEN RETURN false;END IF;
 PERFORM a.id FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task ORDER BY a.id FOR SHARE OF a;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id
  AND a.id=i.artifact_id AND a.sha256=i.sha256 WHERE i.tenant_id=p_tenant AND i.task_id=p_task
  AND (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL
   OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 RETURN t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp() AND s.lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
