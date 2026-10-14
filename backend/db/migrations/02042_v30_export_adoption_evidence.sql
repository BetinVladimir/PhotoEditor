-- +goose Up
-- Persist prepared export evidence under the scheduler's exact lease. This
-- function has no archive IO and does not create links or terminal state.
-- +goose StatementBegin
CREATE FUNCTION photo.save_v30_export_adoption_evidence(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,p_body text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;saved jsonb;frozen jsonb;w photo.v2_workflows%ROWTYPE;
 r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
 existing photo.v30_interchange_export_evidence%ROWTYPE;ref jsonb;source jsonb;
 meta record;ordinal integer;contract_name text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 16777216
  OR coalesce(p_digest,'') !~ '^[0-9a-f]{64}$'
  OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM p_digest
  OR p_tenant IS NULL THEN RETURN false;END IF;
 e:=p_body::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(e))<>17
  OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR e->>'tenant_id' IS DISTINCT FROM p_tenant::text
  OR e->>'actor_id' IS DISTINCT FROM p_actor::text
  OR e->>'request_sha256' IS DISTINCT FROM p_request_sha THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tenants tenant WHERE tenant.id=p_tenant AND tenant.status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR UPDATE;
 SELECT response_body INTO saved FROM photo.idempotency_records
  WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
   AND route='Interchange.Export.Pending' AND key=p_key AND request_hash=p_request_sha
   AND response_status IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run FOR UPDATE;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task FOR SHARE;
 IF w.id IS NULL OR saved IS NULL OR r.id IS NULL OR t.id IS NULL OR z.id IS NULL
  OR w.v30_export_adoption_token IS DISTINCT FROM p_token
  OR (w.v30_export_adoption_retry_after>clock_timestamp()) IS DISTINCT FROM true
  OR t.canonical_result_id IS DISTINCT FROM p_result
  OR photo.v30_export_adoption_claim_current(p_tenant,p_workflow,p_run,p_task,p_result,
   p_actor,p_key,p_request_sha,p_token) IS DISTINCT FROM true THEN RETURN false;END IF;
 frozen:=saved->'frozen';
 IF frozen IS NULL OR e->>'format' IS DISTINCT FROM frozen->'request'->>'format'
  OR coalesce(e->>'frozen_sha256','') !~ '^[0-9a-f]{64}$'
  OR e->'identity' IS DISTINCT FROM frozen->'identity'
  OR e->'identity' IS DISTINCT FROM jsonb_build_object(
   'Run',p_run,'Workflow',p_workflow,'Task',p_task,'Snapshot',w.input_snapshot_id,
   'Params',photo.v30_uuid5(p_task,'interchange:params:1'))
  OR coalesce(e->>'manifest_sha256','') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(e->'documents') IS DISTINCT FROM 'array'
  OR jsonb_array_length(e->'documents') NOT BETWEEN 3 AND 52
  OR jsonb_typeof(e->'files') IS DISTINCT FROM 'array'
  OR jsonb_array_length(e->'files') NOT BETWEEN 1 AND 30000
  OR jsonb_typeof(e->'sources') IS DISTINCT FROM 'array'
  OR jsonb_array_length(e->'sources')>30000 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token)
 WHERE artifact_id=(e->'root'->>'artifact_id')::uuid AND sha256=e->'root'->>'sha256';
 IF NOT FOUND THEN RETURN false;END IF;
 IF e->'documents'->0 IS DISTINCT FROM e->'root'
  OR jsonb_array_length(z.outputs)<>jsonb_array_length(e->'documents')+1
  OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(e->'documents'))
   <>jsonb_array_length(e->'documents')
  OR e->'documents' @> jsonb_build_array(e->'bundle') THEN RETURN false;END IF;
 ordinal:=0;
 FOR ref IN SELECT value FROM jsonb_array_elements(e->'documents') LOOP
  contract_name:=CASE ordinal WHEN 0 THEN 'InterchangeResult'
   WHEN 1 THEN 'MappingReport' ELSE 'MappingShard' END;
  PERFORM 1 FROM photo.v30_export_adoption_output(p_tenant,p_workflow,p_run,p_task,p_result,
   p_actor,p_key,p_request_sha,p_token,(ref->>'artifact_id')::uuid,ref->>'sha256',contract_name);
  IF NOT FOUND THEN RETURN false;END IF;
  ordinal:=ordinal+1;
 END LOOP;
 SELECT * INTO meta FROM photo.v30_export_adoption_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,(e->'bundle'->>'artifact_id')::uuid,
  e->'bundle'->>'sha256','InterchangeArchive');
 IF NOT FOUND OR meta.bucket IS DISTINCT FROM e->>'bucket'
  OR meta.object_key IS DISTINCT FROM e->>'object_key'
  OR meta.object_version IS DISTINCT FROM e->>'object_version'
  OR meta.bytes IS DISTINCT FROM (e->>'archive_bytes')::bigint THEN RETURN false;END IF;
 IF frozen->'request'->>'format'='photoeditor_bundle' THEN
  IF jsonb_array_length(e->'sources')<1 THEN RETURN false;END IF;
 ELSIF jsonb_array_length(e->'sources')<>0 THEN RETURN false;END IF;
 IF (SELECT count(DISTINCT value->'ref'->>'artifact_id') FROM jsonb_array_elements(e->'sources'))
  <>jsonb_array_length(e->'sources') THEN RETURN false;END IF;
 FOR source IN SELECT value FROM jsonb_array_elements(e->'sources') LOOP
  PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
   p_actor,p_key,p_request_sha,p_token,(source->'ref'->>'artifact_id')::uuid,
   source->'ref'->>'sha256',source->>'contract');
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 SELECT * INTO existing FROM photo.v30_interchange_export_evidence
  WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF existing.run_id IS NOT NULL THEN
  RETURN existing.result_id=p_result AND existing.root_id=(e->'root'->>'artifact_id')::uuid
   AND existing.root_sha=e->'root'->>'sha256' AND existing.request_sha=p_request_sha
   AND existing.evidence_sha=p_digest AND existing.evidence_jcs=p_body;
 END IF;
 INSERT INTO photo.v30_interchange_export_evidence(
  tenant_id,run_id,result_id,root_id,root_sha,request_sha,evidence_sha,evidence_jcs)
 VALUES(p_tenant,p_run,p_result,(e->'root'->>'artifact_id')::uuid,
  e->'root'->>'sha256',p_request_sha,p_digest,p_body);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v30_export_adoption_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v30_export_adoption_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.save_v30_export_adoption_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
