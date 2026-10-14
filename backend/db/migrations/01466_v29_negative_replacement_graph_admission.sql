-- +goose Up
ALTER TABLE photo.runs ADD COLUMN v29_negative_replacement_queued_receipt jsonb
 CHECK(v29_negative_replacement_queued_receipt IS NULL OR (jsonb_typeof(v29_negative_replacement_queued_receipt)='object' AND pg_column_size(v29_negative_replacement_queued_receipt)<=65536));
-- +goose StatementBegin
CREATE FUNCTION photo.seal_v29_negative_replacement_graph(p_tenant uuid,p_run uuid,p_task uuid,p_descriptor text,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;request jsonb;workflow uuid;transport jsonb;manifest_body text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.v29_negative_replacement_queued_receipt IS NOT NULL
 OR r.v29_negative_replacement_descriptor_jcs IS DISTINCT FROM p_descriptor THEN RETURN false;END IF;
 request:=(r.config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb;plan:=p_descriptor::jsonb;
 IF plan IS NULL OR plan->'task'->>'id' IS DISTINCT FROM p_task::text THEN RETURN false;END IF;
 SELECT id INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND project_id=(request->'owner'->>'project_id')::uuid
 AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 transport:=photo.read_v29_negative_replacement_published_transport(p_tenant,p_run,p_task,p_descriptor,p_proof);
 IF transport IS NULL THEN RETURN false;END IF;
 manifest_body:=photo.v27_semantic_policy_json(jsonb_build_object('schema_version',1,'task_id',p_task,'entry_count',transport->'count','entries_sha',transport->>'sha256','chunks',transport->'chunks'));
 -- Complete exact registered-row replay, not only a generic closure flag.
 IF photo.register_v29_negative_replacement_transport(p_tenant,p_run,p_descriptor,p_proof,transport,manifest_body) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM operation FROM photo.operation_registry WHERE operation='optical.negative' AND version='1' AND resource_class='cpu'
 AND v2_enabled AND backends ? (plan->'task'->'execution_profile'->>'backend') FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM operation FROM photo.operation_output_policies WHERE operation='optical.negative' AND version='1' ORDER BY role FOR SHARE;
 -- Bypass only the negative-specific public denial; retain the full previous
 -- registry/closure/quota/activation/event/outbox implementation chain.
 IF photo.seal_v2_graph_pre_v29_negative(p_tenant,p_run) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF photo.v29_negative_replacement_member(p_tenant,(request->'owner'->>'project_id')::uuid,
 (request->'owner'->>'actor_id')::uuid,(request->'owner'->>'auth_session_id')::uuid,
 (request->'identity'->>'review_id')::uuid,request->'identity'->>'decision_hash',
 (request->'identity'->>'original_task_id')::uuid,p_task,p_run) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'V29_REPLACEMENT_MEMBERSHIP_AFTER_SEAL' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET v29_negative_replacement_queued_receipt=jsonb_build_object('schema_version',2,'run_id',p_run,'workflow_id',workflow,'replacement_identity_sha',request->>'identity_sha',
 'review_id',request->'identity'->>'review_id','decision_hash',request->'identity'->>'decision_hash','original_task_id',request->'identity'->>'original_task_id','original_run_id',request->'identity'->>'original_run_id',
 'task_id',p_task,'state','queued','actor_id',request->'owner'->>'actor_id','auth_session_id',request->'owner'->>'auth_session_id',
 'operation','optical.negative','descriptor_sha',encode(sha256(convert_to(p_descriptor,'UTF8')),'hex'),
 'qualification',plan->'task'->'execution_profile'->'negative_qualification','transport',transport)
 WHERE tenant_id=p_tenant AND id=p_run AND graph_sealed AND v29_negative_replacement_queued_receipt IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION 'V29_NEGATIVE_REPLACEMENT_RECEIPT_RACE' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.seal_v29_negative_replacement_graph(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v29_negative_replacement_graph(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v29_negative_replacement_graph(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
