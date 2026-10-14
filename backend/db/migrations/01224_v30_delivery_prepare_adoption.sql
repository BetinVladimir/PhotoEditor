-- +goose Up
-- Dedicated commit is mandatory: generic adoption must not finalize prepare
-- without creating the exact slot inventory and reopening the parent graph.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_prepare_adoption_params(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(artifact_id uuid,sha256 text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256 FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 JOIN photo.v2_task_inputs i ON i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='Params'
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='v2.delivery.prepare' AND t.operation_version='1'
 AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.canonical_result_id=p_result
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 65536
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v2_adoption_context(p_tenant,p_task))
$$;
ALTER FUNCTION photo.v30_delivery_prepare_adoption_params(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_prepare_adoption_params(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_prepare_adoption_params(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;

ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_delivery;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_delivery(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='v2.delivery.prepare') THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_pre_delivery(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;

CREATE FUNCTION photo.adopt_v30_delivery_prepared(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_inventory jsonb,p_digest text,p_params_json text,p_body text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;d photo.delivery_packages%ROWTYPE;
 proof jsonb;frozen jsonb;snapshot jsonb;expected jsonb;params_sha text;n integer;slots integer;
BEGIN
 IF p_params_json IS NULL OR octet_length(p_params_json) NOT BETWEEN 2 AND 65536
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id FOR SHARE;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=p_tenant AND workflow_id=w.id FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested
 OR t.id IS NULL OR t.operation<>'v2.delivery.prepare' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.state<>'succeeded' OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.canonical_result_id IS DISTINCT FROM p_result OR t.cancel_requested
 OR w.id IS NULL OR w.workflow_type<>'delivery' OR w.renderer_version<>'v2.2'
 OR d.id IS NULL OR d.state<>'building' OR d.project_id<>w.project_id OR d.snapshot_id<>w.input_snapshot_id THEN RETURN false;END IF;
 params_sha:=encode(sha256(convert_to(p_params_json,'UTF8')),'hex');
 SELECT b.v30_delivery_proof INTO proof FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=r.id
 AND b.owner_id=t.id AND b.purpose='task_params' AND b.artifact_id=t.params_artifact_id AND b.sha256=params_sha
 AND b.bytes=octet_length(p_params_json) AND b.state='ready' FOR SHARE;
 IF proof IS NULL OR photo.v30_delivery_prepare_source_ready(p_tenant,r.id,t.id,t.params_artifact_id,params_sha,p_params_json) IS DISTINCT FROM true THEN RETURN false;END IF;
 frozen:=(proof->>'frozen_input_jcs')::jsonb;snapshot:=(proof->>'snapshot_jcs')::jsonb;
 n:=jsonb_array_length(snapshot->'items');slots:=n*jsonb_array_length(frozen->'presets');
 IF n NOT BETWEEN 1 AND 5000 OR slots NOT BETWEEN 1 AND 80000 THEN RETURN false;END IF;
 expected:=jsonb_build_object('schema_version',1,'input',(p_params_json::jsonb)->'input','snapshot_digest',frozen->>'snapshot_digest','item_count',n,'slot_count',slots);
 IF p_body::jsonb IS DISTINCT FROM expected THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results z JOIN photo.artifacts a ON a.tenant_id=z.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid
 WHERE z.tenant_id=p_tenant AND z.id=p_result AND z.task_id=t.id AND a.sha256=z.outputs->0->>'sha256'
 AND a.sha256=encode(sha256(convert_to(p_body,'UTF8')),'hex') AND a.bytes=octet_length(p_body)
 AND a.media_type='application/json' AND a.output_role='document') THEN RETURN false;END IF;
 -- Generic adoption provides output-upload/fence/physical readiness checks and
 -- exact-inventory replay protection. Its event/outbox commits with the barrier.
 IF NOT photo.adopt_v2_plan_pre_delivery(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest) THEN RETURN false;END IF;
 IF NOT r.graph_sealed OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=r.id)
 OR EXISTS(SELECT 1 FROM photo.delivery_items WHERE tenant_id=p_tenant AND package_id=d.id)
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=r.id) THEN
  RAISE EXCEPTION 'DELIVERY_PREPARATION_GRAPH_INVALID' USING ERRCODE='23514';END IF;
 INSERT INTO photo.delivery_items(tenant_id,project_id,package_id,ordinal,preset_id,preset_sha,required,state)
 SELECT p_tenant,d.project_id,d.id,(item->>'ordinal')::integer,(preset->>'preset_id')::uuid,preset->>'preset_sha',(preset->>'required')::boolean,'pending'
 FROM jsonb_array_elements(snapshot->'items') item CROSS JOIN jsonb_array_elements(frozen->'presets') preset;
 INSERT INTO photo.graph_expansions(tenant_id,run_id,parent_task_id,kind,state,cursor,expected_count)
 VALUES(p_tenant,r.id,t.id,'v2.delivery/1','open','0',slots);
 UPDATE photo.runs SET graph_sealed=false,revision=revision+1 WHERE tenant_id=p_tenant AND id=r.id
 AND status='running' AND graph_sealed AND NOT cancel_requested AND revision<9007199254740991;
 IF NOT FOUND THEN RAISE EXCEPTION 'DELIVERY_PREPARATION_BARRIER_RACE' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_v30_delivery_prepared(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_delivery_prepared(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_delivery_prepared(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
