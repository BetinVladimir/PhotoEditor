-- +goose Up
-- Replacement detector transport retains exact immutable bytes; no dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_replacement_detector_transport_body_ready(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_body text,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; d photo.backend_documents%ROWTYPE;c photo.backend_documents%ROWTYPE;
 entry record; count_entries integer:=0; entries_text text:='';chunk_body text;expected text;task_body jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536
 OR p_purpose IS NULL OR p_purpose NOT IN('v2_input_chunk_0','v2_input_manifest') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_owner FOR UPDATE;
 IF NOT FOUND OR t.state<>'blocked' OR t.cancel_requested OR t.canonical_result_id IS NOT NULL THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF NOT FOUND OR d.state<>'ready' OR d.artifact_id IS DISTINCT FROM t.params_artifact_id THEN RETURN false;END IF;
 task_body:=jsonb_build_object('id',t.id,'node_key',t.node_key,'operation',t.operation,'operation_version',t.operation_version,
 'resource_class',t.resource_class,'timeout_seconds',600,'params',jsonb_build_object('artifact_id',d.artifact_id,'sha256',d.sha256),
 'inputs',t.inputs,'input_digest',t.input_digest,'execution_profile',t.execution_profile);
 IF photo.materialize_v29_replacement_detector_task(p_tenant,p_run,task_body,p_proof) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOR entry IN SELECT x.id,x.sha,x.contract FROM (
  SELECT d.artifact_id AS id,d.sha256 AS sha,'PerspectiveLineDetectionParams'::text AS contract
  UNION ALL
  SELECT (v->>'artifact_id')::uuid,v->>'sha256',CASE WHEN n=1 THEN 'WorkingImage' ELSE 'NpyArray' END
  FROM jsonb_array_elements(t.inputs) WITH ORDINALITY a(v,n)
 ) x ORDER BY x.id LOOP
  count_entries:=count_entries+1;
  IF count_entries>132 OR entry.sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  IF entries_text<>'' THEN entries_text:=entries_text||',';END IF;
  entries_text:=entries_text||format('{"contract":"%s","ref":{"artifact_id":"%s","sha256":"%s"}}',entry.contract,entry.id,entry.sha);
 END LOOP;
 IF count_entries<3 THEN RETURN false;END IF;
 chunk_body:=format('{"entries":[%s],"schema_version":1}',entries_text);
 IF p_purpose='v2_input_chunk_0' THEN RETURN p_body=chunk_body;END IF;
 SELECT * INTO c FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='v2_input_chunk_0' FOR UPDATE;
 IF NOT FOUND OR c.state<>'ready' OR c.v29_perspective_lines_transport IS DISTINCT FROM chunk_body
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=c.artifact_id
 AND a.sha256=c.sha256 AND a.bytes=c.bytes AND a.bucket=c.bucket AND a.object_version=c.object_version
 AND a.object_version NOT IN('','null') AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||c.artifact_id
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='v2_input_chunk_0'
 AND a.media_type='application/json' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 expected:=format('{"chunks":[{"artifact_id":"%s","sha256":"%s"}],"entries_sha":"%s","entry_count":%s,"schema_version":1,"task_id":"%s"}',
 c.artifact_id,c.sha256,encode(sha256(convert_to('['||entries_text||']','UTF8')),'hex'),count_entries,p_owner);
 RETURN p_body=expected;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_replacement_detector_transport_body_ready(uuid,uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_replacement_detector_transport_body_ready(uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway;

-- +goose StatementEnd

-- Exact saved Params, existing immutable document lifecycle; no queue activation.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v29_replacement_detector_transport(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_sha text,p_size bigint,p_bucket text,p_body text,p_proof jsonb)
RETURNS TABLE(artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; d photo.backend_documents%ROWTYPE; plan jsonb;
 capacity numeric; used numeric; aid uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_owner IS NULL OR p_purpose IS NULL OR p_purpose NOT IN('v2_input_chunk_0','v2_input_manifest')
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536
 OR p_size IS DISTINCT FROM octet_length(p_body)::bigint
 OR p_sha IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex')
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR NOT (r.config_snapshot ? 'v29_perspective_replacement_detection_jcs') OR r.status<>'draft'
 OR r.graph_sealed OR r.cancel_requested THEN RETURN;END IF;
 plan:=(r.config_snapshot->>'v29_perspective_replacement_detection_jcs')::jsonb;
 IF plan->>'task_id' IS DISTINCT FROM p_owner::text
 OR photo.v29_replacement_detector_transport_body_ready(p_tenant,p_run,p_owner,p_purpose,p_body,p_proof) IS DISTINCT FROM true THEN RETURN;END IF;
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run
 AND state='reserved' AND principal_type='user' AND principal_id=r.created_by AND bytes=4194304 AND files=0
 AND expires_at>clock_timestamp() FOR SHARE;IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run
 AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_size OR d.bucket IS DISTINCT FROM p_bucket
  OR d.state NOT IN('pending','ready') OR d.v29_perspective_lines_transport IS DISTINCT FROM p_body THEN RETURN;END IF;
  IF d.state='ready' AND NOT EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=p_sha AND a.bytes=p_size
   AND a.bucket=p_bucket AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
   AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
   AND a.output_role=p_purpose AND a.media_type='application/json' AND a.state='ready'
   AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN;END IF;
  RETURN QUERY SELECT d.artifact_id,d.token,d.state='ready';RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_size>capacity OR used+p_size>9007199254740991 THEN RETURN;END IF;
 -- Document reservation is separate from the output run reservation and is
 -- consumed by document completion, matching the existing publisher protocol.
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,v29_perspective_lines_transport)
 VALUES(p_tenant,p_run,p_owner,p_purpose,aid,gen_random_uuid(),p_sha,p_size,p_bucket,p_body)
 RETURNING * INTO d;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_size WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT d.artifact_id,d.token,false;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.reserve_v29_replacement_detector_transport(uuid,uuid,uuid,text,text,bigint,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v29_replacement_detector_transport(uuid,uuid,uuid,text,text,bigint,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.reserve_v29_replacement_detector_transport(uuid,uuid,uuid,text,text,bigint,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- Version-pinned original object completion after IO, repeating live authority.
-- +goose StatementBegin
CREATE FUNCTION photo.complete_v29_replacement_detector_transport(p_tenant uuid,p_run uuid,p_owner uuid,
 p_purpose text,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text,p_body text,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; ticket record; target text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 OR p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
 AND owner_id=p_owner AND purpose=p_purpose FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token
 OR d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 SELECT * INTO ticket FROM photo.reserve_v29_replacement_detector_transport(p_tenant,p_run,p_owner,p_purpose,p_sha,p_bytes,p_bucket,p_body,p_proof);
 IF NOT FOUND OR ticket.artifact_id IS DISTINCT FROM p_artifact OR ticket.token IS DISTINCT FROM p_token THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 IF d.state='ready' THEN RETURN d.object_version=p_version;END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),p_purpose);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes
 WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'LINE_DETECTION_DOCUMENT_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v29_replacement_detector_transport(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v29_replacement_detector_transport(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.complete_v29_replacement_detector_transport(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
