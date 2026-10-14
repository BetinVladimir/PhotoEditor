-- +goose Up
-- Draft-only container for pending-authorized documents. No task can dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v30_delivery_create_run(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_config text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;params jsonb;policy jsonb;scope jsonb;config jsonb;expected jsonb;
 rid uuid;wid uuid;project uuid;snapshot uuid;existing photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_config IS NULL OR octet_length(p_config) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='POST /delivery-packages' AND key=p_key AND request_hash=p_request_sha
 AND response_status IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
 IF saved IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;

 SELECT value INTO params FROM jsonb_array_elements(saved->'documents') WHERE value->>'purpose'='task_params';
 SELECT value INTO policy FROM jsonb_array_elements(saved->'documents') WHERE value->>'purpose'='v30_delivery_policy';
 IF params IS NULL OR policy IS NULL THEN RETURN false;END IF;
 scope:=photo.v30_delivery_create_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,
  (params->'ref'->>'artifact_id')::uuid,'task_params',params->>'body_jcs');
 IF scope IS NULL THEN RETURN false;END IF;
 rid:=(scope->>'run')::uuid;wid:=photo.v30_uuid5(rid,'workflow');project:=(scope->>'project')::uuid;snapshot:=(scope->>'snapshot')::uuid;
 expected:=jsonb_build_object('schema_version',1,'project_id',project,'snapshot_id',snapshot,
  'workflow_type','delivery','request_sha',p_request_sha,'policy_sha',policy->'ref'->>'sha256');
 config:=p_config::jsonb;IF config IS DISTINCT FROM expected THEN RETURN false;END IF;
 SELECT * INTO existing FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 IF FOUND THEN
  SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=rid;
  RETURN existing.kind='v2' AND existing.created_by=p_actor AND existing.status='draft' AND NOT existing.graph_sealed
   AND NOT existing.cancel_requested AND existing.input_snapshot_id=snapshot AND existing.config_snapshot=config
   AND existing.config_hash=encode(sha256(convert_to(p_config,'UTF8')),'hex') AND w.id=wid AND w.project_id=project
   AND w.workflow_type='delivery' AND w.policy_sha=policy->'ref'->>'sha256';
 END IF;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE id=rid) THEN RETURN false;END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(rid,p_tenant,'v2','draft',snapshot,config,encode(sha256(convert_to(p_config,'UTF8')),'hex'),p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(wid,p_tenant,project,rid,snapshot,'delivery','v2.2',policy->'ref'->>'sha256');
 -- Graph/output quota is reserved later with blocked preparation materialization;
 -- immutable document reservations must account for their own bytes separately.
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.begin_v30_delivery_create_run(uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v30_delivery_create_run(uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.begin_v30_delivery_create_run(uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
