-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v30_interchange_parse(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_params_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;rid uuid;wid uuid;tid uuid;pid uuid;project uuid;ticket jsonb;scope jsonb;receipt photo.idempotency_records%ROWTYPE;t photo.tasks%ROWTYPE;proof jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='Interchange.Parse.Pending' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
 IF saved IS NULL THEN RETURN NULL;END IF;
 project:=(saved->'request'->>'project_id')::uuid;
 IF photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL AND download_allowed) THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');
 tid:=photo.v30_uuid5(wid,'v2.interchange.parse:0');pid:=photo.v30_uuid5(tid,'interchange:params:1');
 ticket:=jsonb_build_object('run_id',rid,'workflow_id',wid,'state','queued');
 SELECT * INTO receipt FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='Interchange.Parse.Ticket' AND key=p_key FOR UPDATE;
 IF FOUND THEN
  IF receipt.request_hash IS DISTINCT FROM p_request_sha OR receipt.expires_at<=clock_timestamp() OR receipt.response_status IS DISTINCT FROM 202
  OR receipt.response_body IS DISTINCT FROM ticket OR receipt.resource_id IS DISTINCT FROM wid THEN RETURN NULL;END IF;
  RETURN jsonb_build_object('ticket',ticket,'replayed',true);
 END IF;
 scope:=photo.v30_interchange_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,pid,'task_params',p_params_body);
 IF scope IS NULL THEN RETURN NULL;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=rid AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested
 AND created_by=p_actor FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid AND id=tid FOR UPDATE;
 IF t.id IS NULL OR t.state IS DISTINCT FROM 'blocked' OR t.attempt<>0 OR t.cancel_requested OR t.operation IS DISTINCT FROM 'v2.interchange.parse'
 OR t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu' OR t.params_artifact_id IS DISTINCT FROM pid
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=rid)
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=rid)
 OR photo.v2_input_closure_ready(tid) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 -- Reuse the complete current API proof/source validation in this transaction.
 IF photo.save_v30_interchange_parse_proof(p_tenant,p_actor,p_session,project,rid,tid,pid,scope->>'sha',p_params_body) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=rid
 AND principal_type='user' AND principal_id=p_actor AND state='reserved' AND bytes=(t.execution_profile->'resource_estimate'->>'output_bytes')::bigint
 AND expires_at>clock_timestamp()) THEN RETURN NULL;END IF;
 IF photo.seal_v2_graph(p_tenant,rid) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'INTERCHANGE_INITIAL_ACTIVATION_REFUSED' USING ERRCODE='23514';
 END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,response_status,response_body,resource_id,expires_at)
 VALUES(p_tenant,'user',p_actor,'Interchange.Parse.Ticket',p_key,p_request_sha,202,ticket,wid,clock_timestamp()+interval '24 hours');
 RETURN jsonb_build_object('ticket',ticket,'replayed',false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.activate_v30_interchange_parse(uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_interchange_parse(uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_interchange_parse(uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
