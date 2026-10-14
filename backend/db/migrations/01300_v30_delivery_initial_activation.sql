-- +goose Up
-- Final API-only transition. Graph sealing/outbox and public ticket commit in
-- the same transaction; physical document publication has already completed.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v30_delivery_create(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ledger photo.idempotency_records%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;t photo.tasks%ROWTYPE;
 rid uuid;wid uuid;owner uuid;package uuid;params jsonb;proof jsonb;ticket jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR coalesce(p_request_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 SELECT * INTO ledger FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='POST /delivery-packages' AND key=p_key FOR UPDATE;
 IF NOT FOUND OR ledger.request_hash IS DISTINCT FROM p_request_sha OR ledger.expires_at<=clock_timestamp() THEN RETURN NULL;END IF;
 rid:=photo.v30_uuid5(p_tenant,'delivery:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');
 owner:=photo.v30_uuid5(wid,'v2.delivery.prepare:0');package:=photo.v30_uuid5(wid,'delivery:package:1');
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=wid AND run_id=rid AND workflow_type='delivery' FOR SHARE;
 IF w.id IS NULL OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id AND archived_at IS NULL AND download_allowed) THEN RETURN NULL;END IF;
 ticket:=jsonb_build_object('run_id',rid,'workflow_id',wid,'state','queued');
 IF ledger.response_status IS NOT NULL THEN
  IF ledger.response_status<>202 OR ledger.resource_id IS DISTINCT FROM package OR ledger.response_body IS DISTINCT FROM ticket THEN RETURN NULL;END IF;
  RETURN jsonb_build_object('ticket',ticket,'replayed',true);
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid AND id=owner FOR UPDATE;
 IF r.id IS NULL OR r.created_by IS DISTINCT FROM p_actor OR r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR r.cancel_requested
 OR t.id IS NULL OR t.state IS DISTINCT FROM 'blocked' OR t.attempt<>0 OR t.cancel_requested
 OR t.operation IS DISTINCT FROM 'v2.delivery.prepare' OR t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=rid)
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=rid) THEN RETURN NULL;END IF;
 SELECT value INTO params FROM jsonb_array_elements(ledger.response_body->'documents') WHERE value->>'purpose'='task_params';
 IF params IS NULL OR t.params_artifact_id IS DISTINCT FROM (params->'ref'->>'artifact_id')::uuid THEN RETURN NULL;END IF;
 SELECT v30_delivery_proof INTO proof FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=rid AND owner_id=owner
 AND artifact_id=t.params_artifact_id AND sha256=params->'ref'->>'sha256' AND purpose='task_params' AND state='ready' FOR SHARE;
 IF proof IS NULL OR proof->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR photo.v30_delivery_prepare_source_ready(p_tenant,rid,owner,t.params_artifact_id,params->'ref'->>'sha256',params->>'body_jcs') IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(owner) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM id FROM photo.delivery_packages WHERE tenant_id=p_tenant AND id=package AND workflow_id=wid AND project_id=w.project_id
 AND snapshot_id=w.input_snapshot_id AND policy_artifact_id=photo.v30_uuid5(wid,'delivery:policy:1')
 AND state='building' AND manifest_artifact_id IS NULL AND published_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF photo.seal_v2_graph(p_tenant,rid) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'DELIVERY_INITIAL_ACTIVATION_REFUSED' USING ERRCODE='23514';
 END IF;
 UPDATE photo.idempotency_records SET response_status=202,response_body=ticket,resource_id=package
 WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor AND route='POST /delivery-packages' AND key=p_key;
 RETURN jsonb_build_object('ticket',ticket,'replayed',false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.activate_v30_delivery_create(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_delivery_create(uuid,uuid,uuid,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_delivery_create(uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
