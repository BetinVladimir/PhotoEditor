-- +goose Up
-- Private empty draft containers only; task publication/activation is separate.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v31_signing_draft(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_config text,p_inputs text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ledger photo.idempotency_records%ROWTYPE;saved jsonb;params jsonb;inputs jsonb;expected_inputs jsonb;config jsonb;expected_config jsonb;
 wid uuid;rid uuid;sid uuid;project uuid;digest text;item jsonb;ord bigint:=0;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;s photo.snapshots%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_config IS NULL OR octet_length(p_config) NOT BETWEEN 2 AND 65536 OR p_inputs IS NULL OR octet_length(p_inputs) NOT BETWEEN 2 AND 16384 THEN RETURN false;END IF;
 SELECT * INTO ledger FROM photo.idempotency_records x WHERE x.tenant_id=p_tenant AND x.principal_type='user' AND x.principal_id=p_actor
 AND x.route='Provenance.Sign.Pending' AND x.key=p_key AND x.request_hash=p_request_sha AND x.response_status IS NULL AND x.expires_at>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR ledger.response_body IS NULL THEN RETURN false;END IF;
 saved:=ledger.response_body;params:=(saved->>'params_jcs')::jsonb;inputs:=p_inputs::jsonb;config:=p_config::jsonb;
 wid:=photo.v30_uuid5(p_tenant,'provenance:'||p_actor||':'||p_key);rid:=photo.v30_uuid5(wid,'run');sid:=photo.v30_uuid5(wid,'provenance:artifact-snapshot:1');
 project:=(saved->'source_pins'->>'project')::uuid;digest:=encode(sha256(convert_to(p_inputs,'UTF8')),'hex');
 SELECT jsonb_agg(jsonb_build_object('ref',entry.ref,'contract',entry.contract) ORDER BY (entry.ref->>'artifact_id')::uuid) INTO expected_inputs
 FROM (VALUES(params->'export','QualifiedExport'),(params->'lineage','SigningLineage'),(params->'policy','SigningPolicy')) AS entry(ref,contract);
 IF saved->'schema_version' IS DISTINCT FROM '1'::jsonb OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR saved->>'actor_id' IS DISTINCT FROM p_actor::text OR saved->>'request_sha' IS DISTINCT FROM p_request_sha
 OR saved->>'workflow_id' IS DISTINCT FROM wid::text OR saved->>'run_id' IS DISTINCT FROM rid::text
 OR ledger.resource_id IS DISTINCT FROM rid OR inputs IS DISTINCT FROM expected_inputs OR inputs IS DISTINCT FROM saved->'inputs'
 OR digest IS DISTINCT FROM saved->'task'->>'input_digest' THEN RETURN false;END IF;
 expected_config:=jsonb_build_object('schema_version',1,'project_id',project,'workflow_type','provenance','export_id',saved->>'export_id',
 'request_sha',p_request_sha,'snapshot_id',sid,'snapshot_digest',digest,'params',jsonb_build_object('artifact_id',saved->'task'->>'params_artifact_id','sha256',saved->'task'->>'params_sha'));
 IF config IS DISTINCT FROM expected_config THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 IF photo.v31_signing_admission_ready(p_tenant,p_actor,p_session,(saved->>'export_id')::uuid,saved->'source_pins',saved->>'bucket',saved->>'params_jcs') IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 IF FOUND THEN
  SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=rid;
  SELECT * INTO s FROM photo.snapshots WHERE tenant_id=p_tenant AND id=sid;
  IF r.kind IS DISTINCT FROM 'v2' OR r.status IS DISTINCT FROM 'draft' OR r.created_by IS DISTINCT FROM p_actor OR r.graph_sealed OR r.cancel_requested
  OR r.failure_policy IS DISTINCT FROM 'fail_fast' OR r.input_snapshot_id IS DISTINCT FROM sid OR r.config_snapshot IS DISTINCT FROM config
  OR r.config_hash IS DISTINCT FROM encode(sha256(convert_to(p_config,'UTF8')),'hex') OR w.id IS DISTINCT FROM wid
  OR w.project_id IS DISTINCT FROM project OR w.input_snapshot_id IS DISTINCT FROM sid OR w.workflow_type IS DISTINCT FROM 'provenance'
  OR w.renderer_version IS DISTINCT FROM 'v2.2' OR w.policy_sha IS DISTINCT FROM params->'policy'->>'sha256'
  OR s.id IS NULL OR s.kind IS DISTINCT FROM 'v2' OR s.principal_type IS DISTINCT FROM 'user' OR s.principal_id IS DISTINCT FROM p_actor
  OR s.digest IS DISTINCT FROM digest OR s.item_count IS DISTINCT FROM 3::bigint OR s.expires_at<=clock_timestamp()
  OR EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid) THEN RETURN false;END IF;
  RETURN (SELECT jsonb_agg(x.payload ORDER BY x.ordinal) FROM photo.snapshot_items x WHERE x.tenant_id=p_tenant AND x.snapshot_id=sid) IS NOT DISTINCT FROM inputs;
 END IF;
 IF EXISTS(SELECT FROM photo.runs WHERE id=rid) OR EXISTS(SELECT FROM photo.snapshots WHERE id=sid) OR EXISTS(SELECT FROM photo.v2_workflows WHERE id=wid) THEN RETURN false;END IF;
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at)
 VALUES(sid,p_tenant,'v2','user',p_actor,digest,3,ledger.expires_at);
 FOR item IN SELECT value FROM jsonb_array_elements(inputs) LOOP
  INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload)
  VALUES(p_tenant,sid,ord,(item->'ref'->>'artifact_id')::uuid,item->'ref'->>'sha256',item);ord:=ord+1;
 END LOOP;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(rid,p_tenant,'v2','draft',sid,config,encode(sha256(convert_to(p_config,'UTF8')),'hex'),p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(wid,p_tenant,project,rid,sid,'provenance','v2.2',params->'policy'->>'sha256');
 RETURN true;
END $$;
ALTER FUNCTION photo.begin_v31_signing_draft(uuid,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v31_signing_draft(uuid,uuid,uuid,text,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.begin_v31_signing_draft(uuid,uuid,uuid,text,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
