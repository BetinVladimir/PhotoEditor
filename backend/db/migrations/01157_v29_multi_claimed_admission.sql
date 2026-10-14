-- +goose Up
-- Require the exact durable HTTP request before graph activation; receipt and
-- idempotency response commit atomically with existing activation/events/outbox.
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v29_multi_graph(uuid,uuid,uuid,text,jsonb) RENAME TO seal_v29_multi_graph_pre_claim;
REVOKE ALL ON FUNCTION photo.seal_v29_multi_graph_pre_claim(uuid,uuid,uuid,text,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v29_multi_graph(p_tenant uuid,p_run uuid,p_task uuid,p_descriptor text,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;intent jsonb;expected_hash text;route_name text;claim photo.idempotency_records%ROWTYPE;receipt jsonb;matched integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT config_snapshot->'v29_multi_plan' INTO plan FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR UPDATE;
 IF plan IS NULL OR plan IS DISTINCT FROM p_proof->'plan' OR plan->>'task_id' IS DISTINCT FROM p_task::text THEN RETURN false;END IF;
 intent:=jsonb_build_object('schema_version',1,'snapshot_id',plan->>'snapshot_id','params',(plan->>'params_jcs')::jsonb);
 expected_hash:=encode(sha256(convert_to(photo.v27_semantic_policy_json(jsonb_build_object('project_id',plan->>'project_id','auth_session_id',plan->>'auth_session_id','operation',plan->>'operation','intent',intent)),'UTF8')),'hex');
 route_name:='V29.Multi:'||(plan->>'operation')||':'||(plan->>'project_id');
 SELECT count(*) INTO matched FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND i.principal_id=(plan->>'actor_id')::uuid AND i.route=route_name AND i.resource_id=p_run;
 IF matched<>1 THEN RETURN false;END IF;
 SELECT * INTO claim FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND i.principal_id=(plan->>'actor_id')::uuid AND i.route=route_name AND i.resource_id=p_run FOR UPDATE;
 IF claim.request_hash IS DISTINCT FROM expected_hash OR claim.response_status IS NOT NULL OR claim.response_body IS NOT NULL THEN RETURN false;END IF;
 IF photo.seal_v29_multi_graph_pre_claim(p_tenant,p_run,p_task,p_descriptor,p_proof) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT v29_multi_queued_receipt INTO receipt FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND graph_sealed;
 IF receipt IS NULL THEN RAISE EXCEPTION 'MULTI_MISSING_QUEUED_RECEIPT' USING ERRCODE='23514';END IF;
 UPDATE photo.idempotency_records SET response_status=202,response_body=receipt WHERE tenant_id=p_tenant AND id=claim.id AND request_hash=expected_hash AND resource_id=p_run AND response_status IS NULL AND response_body IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION 'MULTI_IDEMPOTENCY_RESPONSE_RACE' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.seal_v29_multi_graph(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v29_multi_graph(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v29_multi_graph(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.read_v29_multi_queued_receipt(uuid,uuid,uuid,uuid,jsonb) RENAME TO read_v29_multi_queued_receipt_pre_claim;
REVOKE ALL ON FUNCTION photo.read_v29_multi_queued_receipt_pre_claim(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.read_v29_multi_queued_receipt(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_expected jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt jsonb;intent jsonb;expected_hash text;route_name text;claim photo.idempotency_records%ROWTYPE;matched integer;
BEGIN
 -- Existing historical authority proves the current actor/session, complete
 -- immutable plan, original source/qualification and registered closure first.
 receipt:=photo.read_v29_multi_queued_receipt_pre_claim(p_tenant,p_run,p_actor,p_session,p_expected);
 IF receipt IS NULL THEN RETURN NULL;END IF;
 intent:=jsonb_build_object('schema_version',1,'snapshot_id',p_expected->>'snapshot_id','params',(p_expected->>'params_jcs')::jsonb);
 expected_hash:=encode(sha256(convert_to(photo.v27_semantic_policy_json(jsonb_build_object('project_id',p_expected->>'project_id','auth_session_id',p_session,'operation',p_expected->>'operation','intent',intent)),'UTF8')),'hex');
 route_name:='V29.Multi:'||(p_expected->>'operation')||':'||(p_expected->>'project_id');
 SELECT count(*) INTO matched FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND i.principal_id=p_actor AND i.route=route_name AND i.resource_id=p_run;
 IF matched<>1 THEN RETURN NULL;END IF;
 SELECT * INTO claim FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND i.principal_id=p_actor AND i.route=route_name AND i.resource_id=p_run FOR SHARE;
 IF claim.request_hash IS DISTINCT FROM expected_hash THEN RETURN NULL;END IF;
 -- A recreated expired claim may be pending; the independently verified stored
 -- receipt remains historical acknowledgement, never renewed execution.
 IF claim.response_status IS NULL AND claim.response_body IS NULL THEN RETURN receipt;END IF;
 IF claim.response_status IS DISTINCT FROM 202 OR claim.response_body IS DISTINCT FROM receipt THEN RETURN NULL;END IF;
 RETURN receipt;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_multi_queued_receipt(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_multi_queued_receipt(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_multi_queued_receipt(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
