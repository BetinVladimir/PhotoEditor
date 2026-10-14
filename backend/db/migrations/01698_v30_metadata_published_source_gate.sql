-- +goose Up
-- A metadata proof must consume a ready API publication of its exact source.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_published_source_ready(p_tenant uuid,p_parent uuid,p_proof jsonb,p_approved text[]) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;envelope jsonb;s jsonb;project uuid;actor uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_parent
 AND purpose='v30_metadata_source' AND state='ready'
 AND artifact_id=(p_proof->'metadata_source'->>'artifact_id')::uuid;
 IF NOT FOUND OR d.v30_metadata_source_jcs IS NULL OR d.v30_metadata_source_sha IS NULL
 OR d.sha256 IS DISTINCT FROM p_proof->'metadata_source'->>'sha256'
 OR d.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_proof->>'metadata_jcs','UTF8')),'hex')
 OR d.bytes IS DISTINCT FROM octet_length(p_proof->>'metadata_jcs') THEN RETURN false;END IF;
 envelope:=d.v30_metadata_source_jcs::jsonb;s:=envelope->'scope';
 IF photo.v30_metadata_source_publication_valid(d.v30_metadata_source_jcs,d.v30_metadata_source_sha,s,
 p_proof->'metadata_source',p_proof->>'metadata_jcs',p_proof->>'policy',p_approved) IS DISTINCT FROM true
 OR s->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR s->'binding' IS DISTINCT FROM p_proof->'binding'
 OR envelope->>'frozen_jcs' IS DISTINCT FROM p_proof->>'frozen_jcs'
 OR envelope->>'frozen_sha256' IS DISTINCT FROM p_proof->>'frozen_sha256' THEN RETURN false;END IF;
 project:=(s->>'project_id')::uuid;actor:=(s->>'actor_id')::uuid;
 IF NOT EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_parent AND r.kind='v2' AND r.status='running' AND NOT r.cancel_requested
 AND r.created_by=actor AND w.project_id=project AND w.workflow_type='delivery' AND w.renderer_version='v2.2')
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL AND download_allowed)
 THEN RETURN false;END IF;
 -- Session authorization is frozen at API publication. A scheduler does not
 -- impersonate the user or acquire access to private original object bytes.
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(s->'original'->>'artifact_id')::uuid
 AND a.sha256=s->'original'->>'sha256' AND a.bytes=(s->>'original_bytes')::bigint
 AND a.media_type=s->>'original_mime' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_parent||'/documents/'||d.artifact_id
 AND a.media_type='application/json' AND a.output_role='v30_metadata_source' AND a.state='ready'
 AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_published_source_ready(uuid,uuid,jsonb,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_published_source_ready(uuid,uuid,jsonb,text[]) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.pin_v30_delivery_metadata_proof(uuid,uuid,uuid,text,text)'::regprocedure) IS DISTINCT FROM 'e473948ea89a0d5bf124b39fbe3a1834' THEN RAISE EXCEPTION 'METADATA_SOURCE_GATE_DEFINITION_DRIFT';END IF;END $$;
CREATE OR REPLACE FUNCTION photo.pin_v30_delivery_metadata_proof(p_tenant uuid,p_expansion uuid,p_token uuid,p_body text,p_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 proof jsonb;request jsonb;cfg jsonb;task_id uuid;approved text[];ref jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 OR p_sha IS NULL
 OR p_sha IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex') THEN RETURN false;END IF;
 proof:=p_body::jsonb;
 IF jsonb_typeof(proof) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(proof))<>15
 OR proof->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion;
 IF NOT FOUND OR e.kind<>'v30.delivery.metadata/1' OR e.state<>'open' OR e.expected_count<>1
 OR e.expansion_token IS DISTINCT FROM p_token OR p_token IS NULL OR e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=e.run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested OR r.graph_sealed THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion AND run_id=r.id FOR UPDATE;
 IF NOT FOUND OR e.kind<>'v30.delivery.metadata/1' OR e.expected_count<>1 OR e.state<>'open' OR e.expansion_token IS DISTINCT FROM p_token OR e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 request:=r.config_snapshot->'v30_delivery_export';cfg:=(request->>'config_jcs')::jsonb;
 IF proof->'binding' IS DISTINCT FROM request->'binding' OR proof->>'policy' IS DISTINCT FROM cfg->>'metadata_policy'
 OR proof->>'policy' NOT IN('copyright','approved_exif') THEN RETURN false;END IF;
 -- This field must be frozen by authenticated child admission, never by proof.
 IF proof->>'policy'='approved_exif' THEN
  IF jsonb_typeof(r.config_snapshot->'v30_metadata_approved_fields') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved
   FROM jsonb_array_elements_text(r.config_snapshot->'v30_metadata_approved_fields');
 ELSE approved:=ARRAY[]::text[];END IF;
 IF photo.v30_metadata_published_source_ready(p_tenant,(request->>'parent_run_id')::uuid,proof,approved) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF proof->'approved_fields' IS DISTINCT FROM to_jsonb(approved) THEN RETURN false;END IF;
 task_id:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'metadata.write:0');
 IF proof->>'task_id' IS DISTINCT FROM task_id::text OR photo.v30_metadata_write_plan_valid(task_id,proof->'encoded',proof->>'encoded_jcs',
 proof->>'frozen_jcs',proof->>'frozen_sha256',proof->'binding',proof->'metadata_source',proof->>'metadata_jcs',proof->>'policy',approved,
 proof->>'projection_jcs',proof->>'params_jcs') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF proof->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',photo.v30_uuid5(task_id,'delivery:metadata:params:1'),
 'sha256',encode(sha256(convert_to(proof->>'params_jcs','UTF8')),'hex'))
 OR proof->'projection' IS DISTINCT FROM jsonb_build_object('artifact_id',photo.v30_uuid5(task_id,'delivery:metadata:projection:1'),
 'sha256',encode(sha256(convert_to(proof->>'projection_jcs','UTF8')),'hex')) THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=e.parent_task_id FOR SHARE;
 IF NOT FOUND OR t.operation<>'export.encode' OR t.operation_version<>'1' OR t.state<>'succeeded'
 OR t.cancel_requested OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.task_id=t.id AND z.id=t.canonical_result_id
 AND z.outputs @> jsonb_build_array(proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact')) THEN RETURN false;END IF;
 FOREACH ref IN ARRAY ARRAY[proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact',proof->'metadata_source'] LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(ref->>'artifact_id')::uuid
   AND a.sha256=ref->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
   AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 END LOOP;
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF e.v30_metadata_proof_jcs IS NOT NULL THEN RETURN e.v30_metadata_proof_jcs=p_body AND e.v30_metadata_proof_sha=p_sha;END IF;
 UPDATE photo.graph_expansions SET v30_metadata_proof_jcs=p_body,v30_metadata_proof_sha=p_sha WHERE id=e.id;
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_METADATA_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;

DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.v30_metadata_result_ready(uuid,uuid,integer,bigint,uuid)'::regprocedure) IS DISTINCT FROM '07e468de0ea86488859230b0af579850' THEN RAISE EXCEPTION 'METADATA_SOURCE_GATE_DEFINITION_DRIFT';END IF;END $$;
CREATE OR REPLACE FUNCTION photo.v30_metadata_result_ready(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;e photo.graph_expansions%ROWTYPE;
 z photo.task_results%ROWTYPE;proof jsonb;request jsonb;cfg jsonb;approved text[];ref jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.operation<>'metadata.write' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt
 OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result
 OR p_result IS NULL OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested OR NOT r.graph_sealed
 OR t.id<>photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'metadata.write:0') THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=r.id
 AND kind='v30.delivery.metadata/1' AND state='closed';
 IF NOT FOUND OR e.expected_count<>1 OR e.v30_metadata_proof_jcs IS NULL THEN RETURN false;END IF;
 proof:=e.v30_metadata_proof_jcs::jsonb;request:=r.config_snapshot->'v30_delivery_export';
 cfg:=(request->>'config_jcs')::jsonb;
 IF proof->>'task_id' IS DISTINCT FROM t.id::text OR proof->'binding' IS DISTINCT FROM request->'binding'
 OR proof->>'policy' IS DISTINCT FROM cfg->>'metadata_policy' THEN RETURN false;END IF;
 IF proof->>'policy'='approved_exif' THEN
  IF jsonb_typeof(r.config_snapshot->'v30_metadata_approved_fields') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved
  FROM jsonb_array_elements_text(r.config_snapshot->'v30_metadata_approved_fields');
 ELSE approved:=ARRAY[]::text[];END IF;
 IF photo.v30_metadata_published_source_ready(p_tenant,(request->>'parent_run_id')::uuid,proof,approved) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF proof->'approved_fields' IS DISTINCT FROM to_jsonb(approved)
 OR photo.v30_metadata_write_plan_valid(t.id,proof->'encoded',proof->>'encoded_jcs',proof->>'frozen_jcs',
 proof->>'frozen_sha256',proof->'binding',proof->'metadata_source',proof->>'metadata_jcs',proof->>'policy',approved,
 proof->>'projection_jcs',proof->>'params_jcs') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions parent WHERE parent.tenant_id=p_tenant
 AND parent.run_id=(request->>'parent_run_id')::uuid AND parent.kind='v2.delivery/1' AND parent.state='open'
 AND photo.v30_delivery_expansion_ready(parent.id) IS TRUE) THEN RETURN false;END IF;
 IF proof->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',photo.v30_uuid5(t.id,'delivery:metadata:params:1'),
 'sha256',encode(sha256(convert_to(proof->>'params_jcs','UTF8')),'hex'))
 OR proof->'projection' IS DISTINCT FROM jsonb_build_object('artifact_id',photo.v30_uuid5(t.id,'delivery:metadata:projection:1'),
 'sha256',encode(sha256(convert_to(proof->>'projection_jcs','UTF8')),'hex')) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks producer JOIN photo.task_results result
 ON result.tenant_id=producer.tenant_id AND result.task_id=producer.id AND result.id=producer.canonical_result_id
 WHERE producer.tenant_id=p_tenant AND producer.run_id=r.id AND producer.id=e.parent_task_id
 AND producer.operation='export.encode' AND producer.operation_version='1' AND producer.state='succeeded'
 AND NOT producer.cancel_requested AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 AND result.outputs @> jsonb_build_array(proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact')) THEN RETURN false;END IF;
 IF t.params_artifact_id::text IS DISTINCT FROM proof->'params'->>'artifact_id'
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOREACH ref IN ARRAY ARRAY[proof->'params',proof->'projection',proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact'] LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a
   ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
   WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=(ref->>'artifact_id')::uuid
   AND i.sha256=ref->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 END LOOP;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=p_result;
 IF NOT FOUND OR z.attempt IS DISTINCT FROM p_attempt OR z.fencing_token IS DISTINCT FROM p_fence
 OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(z.outputs)<>3 THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(z.outputs) output WHERE NOT EXISTS(
  SELECT 1 FROM photo.artifacts a JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
  WHERE a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
  AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready'
  AND u.sha256=a.sha256 AND u.role=a.output_role
  AND a.output_role IN('encoded_manifest','encoded_image','metadata_report'))) THEN RETURN false;END IF;
 RETURN (SELECT count(DISTINCT a.output_role)=3 FROM jsonb_array_elements(z.outputs) output
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
