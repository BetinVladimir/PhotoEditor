-- +goose Up
-- Stage the deterministic CPU task while the initial graph remains sealed.
-- Its incomplete input closure prevents worker dispatch until atomic commit.
-- Keep execution disabled until the complete orchestration/qualification is installed.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES('v2.semantic.assemble','1','cpu','cpu.semantic_assemble',NULL,600,'["cpu"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
VALUES('v2.semantic.assemble','1','document','["application/json"]',8388608,1);
-- +goose StatementBegin
CREATE FUNCTION photo.stage_v27_semantic_assembly(p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,
 p_task uuid,p_params uuid,p_params_sha text,p_input_digest text,p_candidates jsonb,p_profile jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE; existing photo.tasks%ROWTYPE; expected jsonb; h bytea; expected_id uuid; producer record;
BEGIN
 IF p_params_sha IS NULL OR p_params_sha !~ '^[0-9a-f]{64}$' OR p_input_digest IS NULL OR p_input_digest !~ '^[0-9a-f]{64}$'
 OR p_task IS NULL OR p_params IS NULL OR jsonb_typeof(p_candidates) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_candidates) NOT BETWEEN 1 AND 5000 OR p_profile IS DISTINCT FROM
 '{"backend":"cpu","renderer_version":"v2.2","resource_estimate":{"ram_bytes":268435456,"vram_bytes":0,"scratch_bytes":8388608,"output_bytes":8388608,"pixel_count":0}}'::jsonb THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 PERFORM id FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_expansion FOR UPDATE;
 IF NOT photo.v27_semantic_assembly_lease_live(p_tenant,p_run,p_expansion,p_token) THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',x.artifact_id,'sha256',x.sha256) ORDER BY x.ordinal)
 INTO expected FROM photo.v27_semantic_assembly_inputs(p_tenant,p_run) x;
 IF expected IS NULL OR expected IS DISTINCT FROM p_candidates THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='semantic.transfer')
 <>jsonb_array_length(p_candidates) THEN RETURN false;END IF;
 h:=public.digest(uuid_send(w.id)||convert_to('v2.semantic.assemble:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_task IS DISTINCT FROM expected_id OR NOT EXISTS(SELECT 1 FROM photo.operation_registry
 WHERE operation='v2.semantic.assemble' AND version='1' AND resource_class='cpu' AND backends ? 'cpu')
 OR NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_expansion AND d.purpose='task_params'
 AND d.state='ready' AND d.artifact_id=p_params AND d.sha256=p_params_sha AND a.sha256=d.sha256
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version=d.object_version AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 SELECT * INTO existing FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND node_key='v2.semantic.assemble:0' FOR UPDATE;
 IF existing.id IS NOT NULL THEN
 RETURN existing.id=p_task AND existing.operation='v2.semantic.assemble' AND existing.operation_version='1'
 AND existing.resource_class='cpu' AND existing.params_artifact_id=p_params AND existing.input_digest=p_input_digest
 AND (existing.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha'-'contracts_sha')=p_profile
 AND existing.state='blocked' AND existing.attempt=0 AND NOT existing.cancel_requested;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation<>'semantic.transfer') THEN RETURN false;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(p_task,p_tenant,p_run,'v2.semantic.assemble:0','v2.semantic.assemble','1','cpu','[]',p_params,p_input_digest,p_profile);
 FOR producer IN SELECT task_id FROM photo.v27_semantic_assembly_inputs(p_tenant,p_run) LOOP
 INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition)
 VALUES(p_tenant,p_run,producer.task_id,p_task,'success');
 END LOOP;
 RETURN true;
END $$;
ALTER FUNCTION photo.stage_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
