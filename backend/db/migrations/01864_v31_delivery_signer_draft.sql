-- +goose Up
-- Empty signer draft admitted only from the current Delivery parent lease.
-- No editor session or user idempotency record is fabricated by this path.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v31_delivery_signer_draft(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_source jsonb,p_lineage_bytes bigint,p_config text,p_inputs text,
 p_params text,p_bucket text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;tenant uuid;actor uuid;project uuid;export_id uuid;
 wid uuid;rid uuid;sid uuid;tid uuid;policy_id uuid;policy_ref uuid;
 params_ref uuid;params_sha text;lineage_ref uuid;lineage_sha text;
 config jsonb;params jsonb;inputs jsonb;expected_config jsonb;
 expected_inputs jsonb;input_sha text;request_sha text;
 item jsonb;ord bigint:=0;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 s photo.snapshots%ROWTYPE;
BEGIN
 IF p_config IS NULL OR octet_length(p_config) NOT BETWEEN 2 AND 65536
  OR p_inputs IS NULL OR octet_length(p_inputs) NOT BETWEEN 2 AND 16384
  OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
  OR p_lineage_bytes IS NULL OR p_lineage_bytes NOT BETWEEN 2 AND 8388608
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
  OR jsonb_typeof(p_source) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 scope:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF scope IS NULL THEN RETURN false;END IF;
 tenant:=(scope->>'tenant_id')::uuid;actor:=(scope->>'actor_id')::uuid;
 project:=(scope->>'project_id')::uuid;export_id:=(scope->>'export_id')::uuid;
 wid:=(scope->>'workflow_id')::uuid;rid:=photo.v30_uuid5(wid,'run');
 sid:=photo.v30_uuid5(wid,'provenance:artifact-snapshot:1');
 tid:=(scope->>'task_id')::uuid;policy_id:=(scope->>'policy_id')::uuid;
 policy_ref:=photo.v30_uuid5(tenant,
  'signing:policy:'||policy_id::text||':'||(scope->>'policy_sha'));
 params_ref:=photo.v30_uuid5(tid,'params');
 params_sha:=encode(sha256(convert_to(p_params,'UTF8')),'hex');
 params:=p_params::jsonb;config:=p_config::jsonb;inputs:=p_inputs::jsonb;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object'
  OR jsonb_typeof(config) IS DISTINCT FROM 'object'
  OR jsonb_typeof(inputs) IS DISTINCT FROM 'array'
  OR params->>'schema_version' IS DISTINCT FROM '1'
  OR params->>'signing_policy_id' IS DISTINCT FROM policy_id::text
  OR params->'export' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',scope->>'export_artifact_id','sha256',scope->>'export_sha')
  OR params->'policy' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',policy_ref,'sha256',scope->>'policy_sha') THEN RETURN false;END IF;
 lineage_ref:=(params->'lineage'->>'artifact_id')::uuid;
 lineage_sha:=params->'lineage'->>'sha256';
 IF lineage_sha IS NULL OR lineage_sha !~ '^[0-9a-f]{64}$'
  OR lineage_ref IS DISTINCT FROM photo.v30_uuid5(export_id,
   'signing:lineage:'||lineage_sha)
  OR photo.v31_delivery_lineage_ready(p_expansion,p_token,p_ordinal,
   p_preset,p_source,lineage_ref,lineage_sha,p_lineage_bytes,p_bucket)
   IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',entry.ref,'contract',entry.contract)
  ORDER BY (entry.ref->>'artifact_id')::uuid) INTO expected_inputs
 FROM (VALUES(params->'export','QualifiedExport'),
  (params->'lineage','SigningLineage'),
  (params->'policy','SigningPolicy')) AS entry(ref,contract);
 input_sha:=encode(sha256(convert_to(p_inputs,'UTF8')),'hex');
 request_sha:=encode(sha256(convert_to(format(
  '{"export_id":"%s","signing_policy_id":"%s"}',export_id,policy_id),
  'UTF8')),'hex');
 expected_config:=jsonb_build_object('schema_version',1,'project_id',project,
  'workflow_type','provenance','export_id',export_id,
  'request_sha',request_sha,'snapshot_id',sid,'snapshot_digest',input_sha,
  'params',jsonb_build_object('artifact_id',params_ref,'sha256',params_sha),
  'queue_class','batch');
 IF inputs IS DISTINCT FROM expected_inputs OR config IS DISTINCT FROM expected_config
  OR photo.v31_signing_policy_ready(tenant,policy_id,policy_ref,
   scope->>'policy_sha') IS DISTINCT FROM true THEN RETURN false;END IF;
 -- Keep the same tenant usage -> tenant -> run lock order as V31 publication.
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF photo.v30_delivery_signer_delegation_scope(p_expansion,p_token,
  p_ordinal,p_preset) IS DISTINCT FROM scope THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=tenant AND id=rid FOR UPDATE;
 IF FOUND THEN
  SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=tenant AND run_id=rid;
  SELECT * INTO s FROM photo.snapshots WHERE tenant_id=tenant AND id=sid;
  IF r.kind IS DISTINCT FROM 'v2' OR r.status IS DISTINCT FROM 'draft'
   OR r.created_by IS DISTINCT FROM actor OR r.graph_sealed OR r.cancel_requested
   OR r.failure_policy IS DISTINCT FROM 'fail_fast'
   OR r.input_snapshot_id IS DISTINCT FROM sid
   OR r.config_snapshot IS DISTINCT FROM config
   OR r.config_hash IS DISTINCT FROM encode(sha256(convert_to(p_config,'UTF8')),'hex')
   OR w.id IS DISTINCT FROM wid OR w.project_id IS DISTINCT FROM project
   OR w.input_snapshot_id IS DISTINCT FROM sid
   OR w.workflow_type IS DISTINCT FROM 'provenance'
   OR w.renderer_version IS DISTINCT FROM 'v2.2'
   OR w.policy_sha IS DISTINCT FROM scope->>'policy_sha'
   OR s.id IS NULL OR s.kind IS DISTINCT FROM 'v2'
   OR s.principal_type IS DISTINCT FROM 'user'
   OR s.principal_id IS DISTINCT FROM actor OR s.digest IS DISTINCT FROM input_sha
   OR s.item_count IS DISTINCT FROM 3::bigint
   OR s.expires_at<=clock_timestamp()
   OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=tenant AND run_id=rid)
   THEN RETURN false;END IF;
  RETURN (SELECT jsonb_agg(x.payload ORDER BY x.ordinal)
   FROM photo.snapshot_items x WHERE x.tenant_id=tenant AND x.snapshot_id=sid)
   IS NOT DISTINCT FROM inputs;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE id=rid)
  OR EXISTS(SELECT 1 FROM photo.snapshots WHERE id=sid)
  OR EXISTS(SELECT 1 FROM photo.v2_workflows WHERE id=wid) THEN RETURN false;END IF;
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,
  digest,item_count,expires_at)
 VALUES(sid,tenant,'v2','user',actor,input_sha,3,
  clock_timestamp()+interval '24 hours');
 FOR item IN SELECT value FROM jsonb_array_elements(inputs) LOOP
  INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,
   artifact_id,sha256,payload)
  VALUES(tenant,sid,ord,(item->'ref'->>'artifact_id')::uuid,
   item->'ref'->>'sha256',item);
  ord:=ord+1;
 END LOOP;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,
  config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(rid,tenant,'v2','draft',sid,config,
  encode(sha256(convert_to(p_config,'UTF8')),'hex'),actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,
  input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(wid,tenant,project,rid,sid,'provenance','v2.2',
  scope->>'policy_sha');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_v31_delivery_signer_draft(uuid,uuid,integer,uuid,jsonb,bigint,text,text,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v31_delivery_signer_draft(uuid,uuid,integer,uuid,jsonb,bigint,text,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.begin_v31_delivery_signer_draft(uuid,uuid,integer,uuid,jsonb,bigint,text,text,text,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
