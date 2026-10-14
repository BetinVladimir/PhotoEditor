-- +goose Up
ALTER TABLE photo.backend_documents ADD COLUMN v29_perspective_lines_transport text;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v29_lines_transport_body CHECK(
 v29_perspective_lines_transport IS NULL OR (purpose IN('v2_input_chunk_0','v2_input_manifest')
 AND octet_length(v29_perspective_lines_transport)=bytes AND bytes BETWEEN 2 AND 65536
 AND encode(sha256(convert_to(v29_perspective_lines_transport,'UTF8')),'hex')=sha256));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_lines_transport_body() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v29_perspective_lines_transport IS NOT NULL AND current_user<>'photo_fn_owner' THEN
   RAISE EXCEPTION 'Trusted line transport required' USING ERRCODE='42501';END IF;
 ELSIF OLD.v29_perspective_lines_transport IS DISTINCT FROM NEW.v29_perspective_lines_transport THEN
  RAISE EXCEPTION 'Line transport body is immutable' USING ERRCODE='42501';
 END IF;RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_lines_transport_body() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_lines_transport_body() FROM PUBLIC;
CREATE TRIGGER backend_documents_v29_lines_transport_guard BEFORE INSERT OR UPDATE ON photo.backend_documents
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_lines_transport_body();

-- Caller holds tenant/usage/run locks. Reconstruct original JCS from typed
-- task inputs instead of hashing JSONB's different serialization.
CREATE FUNCTION photo.v29_perspective_lines_transport_body_ready(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_body text)
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
 IF photo.materialize_v29_perspective_line_detection_task(p_tenant,p_run,task_body) IS DISTINCT FROM true THEN RETURN false;END IF;
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
ALTER FUNCTION photo.v29_perspective_lines_transport_body_ready(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_transport_body_ready(uuid,uuid,uuid,text,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
