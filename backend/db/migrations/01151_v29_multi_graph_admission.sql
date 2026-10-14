-- +goose Up
ALTER TABLE photo.runs ADD COLUMN v29_multi_queued_receipt jsonb
 CHECK(v29_multi_queued_receipt IS NULL OR (jsonb_typeof(v29_multi_queued_receipt)='object' AND pg_column_size(v29_multi_queued_receipt)<=65536));
-- +goose StatementBegin
CREATE FUNCTION photo.seal_v29_multi_graph(p_tenant uuid,p_run uuid,p_task uuid,p_descriptor text,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;request jsonb;workflow uuid;transport jsonb;manifest_body text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.v29_multi_queued_receipt IS NOT NULL
 OR r.v29_multi_descriptor_jcs IS DISTINCT FROM p_descriptor THEN RETURN false;END IF;
 plan:=r.config_snapshot->'v29_multi_plan';request:=plan;
 IF plan IS NULL OR coalesce(plan->>'operation','') NOT IN('optical.panorama','optical.focus_stack') OR plan->>'task_id' IS DISTINCT FROM p_task::text THEN RETURN false;END IF;
 SELECT id INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND project_id=(request->>'project_id')::uuid
 AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 transport:=photo.read_v29_multi_published_transport(p_tenant,p_run,p_task,p_descriptor,p_proof);
 IF transport IS NULL THEN RETURN false;END IF;
 manifest_body:=photo.v27_semantic_policy_json(jsonb_build_object('schema_version',1,'task_id',p_task,'entry_count',transport->'count','entries_sha',transport->>'sha256','chunks',transport->'chunks'));
 -- Complete exact registered-row replay, not only a generic closure flag.
 IF photo.register_v29_multi_transport(p_tenant,p_run,p_descriptor,p_proof,transport,manifest_body) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM operation FROM photo.operation_registry WHERE operation=plan->>'operation' AND version='1' AND resource_class='cpu'
 AND v2_enabled AND backends ? (plan->'execution_profile'->>'backend') FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM operation FROM photo.operation_output_policies WHERE operation=plan->>'operation' AND version='1' ORDER BY role FOR SHARE;
 -- Bypass only the multi-specific public denial; retain the full previous
 -- registry/closure/quota/activation/event/outbox implementation chain.
 IF photo.seal_v2_graph_pre_v29_multi(p_tenant,p_run) IS DISTINCT FROM true THEN RETURN false;END IF;
 -- Worker/adopter source gates bind the original actor session. Write only
 -- after all draft rechecks, atomically with activation and the queued receipt.
 UPDATE photo.runs SET config_snapshot=config_snapshot||jsonb_build_object('v29_multi',jsonb_build_object('auth_session_id',request->>'auth_session_id')),
 config_hash=encode(sha256(convert_to((config_snapshot||jsonb_build_object('v29_multi',jsonb_build_object('auth_session_id',request->>'auth_session_id')))::text,'UTF8')),'hex')
 WHERE tenant_id=p_tenant AND id=p_run AND graph_sealed;
 UPDATE photo.runs SET v29_multi_queued_receipt=jsonb_build_object('schema_version',1,'run_id',p_run,'workflow_id',workflow,
 'task_id',p_task,'state','queued','actor_id',request->>'actor_id','auth_session_id',request->>'auth_session_id',
 'operation',plan->>'operation','descriptor_sha',encode(sha256(convert_to(p_descriptor,'UTF8')),'hex'),
 'qualification',plan->'execution_profile'->'multi_qualification','transport',transport)
 WHERE tenant_id=p_tenant AND id=p_run AND graph_sealed AND v29_multi_queued_receipt IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION 'V29_MULTI_RECEIPT_RACE' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.seal_v29_multi_graph(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v29_multi_graph(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v29_multi_graph(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
