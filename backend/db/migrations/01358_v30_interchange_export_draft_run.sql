-- +goose Up
-- Reuse the authorized session snapshot; no snapshot insertion or task activation.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v30_interchange_export_run(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_config text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;frozen jsonb;request jsonb;config jsonb;expected jsonb;snapshot jsonb;
 rid uuid;wid uuid;sid uuid;project uuid;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 120
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$'
 OR p_config IS NULL OR octet_length(p_config) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='Interchange.Export.Pending' AND key=p_key AND request_hash=p_request_sha
 AND response_status IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
 IF saved IS NULL OR saved->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text OR saved->>'actor_id' IS DISTINCT FROM p_actor::text THEN RETURN false;END IF;
 frozen:=saved->'frozen';request:=frozen->'request';sid:=(request->>'snapshot_id')::uuid;project:=(request->>'project_id')::uuid;
 IF sid IS NULL OR project IS NULL OR frozen->>'snapshot_jcs' IS NULL
 OR octet_length(frozen->>'snapshot_jcs') NOT BETWEEN 2 AND 8388608
 OR photo.v25_lens_snapshot_ready(p_tenant,project,sid,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=sid AND kind='v2'
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 snapshot:=(frozen->>'snapshot_jcs')::jsonb;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');
 expected:=jsonb_build_object('schema_version',1,'project_id',project,'snapshot_id',sid,'workflow_type','interchange',
 'request_sha',p_request_sha,'snapshot_digest',snapshot->>'snapshot_digest','snapshot_document',frozen->'snapshot');
 config:=p_config::jsonb;
 IF config IS DISTINCT FROM expected OR snapshot->>'snapshot_digest' IS NULL
 OR snapshot->>'snapshot_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 IF FOUND THEN
  SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=rid;
  IF r.kind IS DISTINCT FROM 'v2' OR r.created_by IS DISTINCT FROM p_actor OR r.status IS DISTINCT FROM 'draft'
  OR r.graph_sealed OR r.cancel_requested OR r.input_snapshot_id IS DISTINCT FROM sid OR r.failure_policy IS DISTINCT FROM 'fail_fast'
  OR r.config_snapshot IS DISTINCT FROM config OR r.config_hash IS DISTINCT FROM encode(sha256(convert_to(p_config,'UTF8')),'hex')
  OR w.id IS DISTINCT FROM wid OR w.project_id IS DISTINCT FROM project OR w.input_snapshot_id IS DISTINCT FROM sid
  OR w.workflow_type IS DISTINCT FROM 'interchange' OR w.renderer_version IS DISTINCT FROM 'v2.2'
  OR w.policy_sha IS DISTINCT FROM p_request_sha THEN RETURN false;END IF;
 ELSE
  IF EXISTS(SELECT 1 FROM photo.runs WHERE id=rid) OR EXISTS(SELECT 1 FROM photo.v2_workflows WHERE id=wid) THEN RETURN false;END IF;
  INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
  VALUES(rid,p_tenant,'v2','draft',sid,config,encode(sha256(convert_to(p_config,'UTF8')),'hex'),p_actor,'fail_fast',false);
  INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
  VALUES(wid,p_tenant,project,rid,sid,'interchange','v2.2',p_request_sha);
 END IF;
 -- Reuse all frozen document/identity/format authority checks after draft creation.
 -- Raising here rolls back inserts in this exception block, including a failed new draft.
 IF photo.v30_interchange_export_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,
 (frozen->'params'->>'artifact_id')::uuid,'task_params',frozen->>'params_jcs') IS NULL THEN
  RAISE EXCEPTION 'EXPORT_DRAFT_INVALID' USING ERRCODE='22023';
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.begin_v30_interchange_export_run(uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v30_interchange_export_run(uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.begin_v30_interchange_export_run(uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
