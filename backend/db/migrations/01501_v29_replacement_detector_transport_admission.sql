-- +goose Up
-- Exact ready transport proof; physical mapped source locks remain mandatory
-- in the Go caller. No workflow, input registration or queue activation here.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_replacement_detector_transport_admission(p_tenant uuid,p_run uuid,p_owner uuid,p_proof jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c photo.backend_documents%ROWTYPE;m photo.backend_documents%ROWTYPE;d photo.backend_documents%ROWTYPE;
 chunk jsonb;manifest jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO c FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='v2_input_chunk_0' FOR UPDATE;
 IF NOT FOUND OR c.state<>'ready' OR c.v29_perspective_lines_transport IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO m FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='v2_input_manifest' FOR UPDATE;
 IF NOT FOUND OR m.state<>'ready' OR m.v29_perspective_lines_transport IS NULL THEN RETURN NULL;END IF;
 -- Replay both publication gates: exact blocked task, saved descriptor, Params,
 -- immutable bytes and live output reservation. No new document is created.
 FOR d IN SELECT * FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner
 AND purpose IN('v2_input_chunk_0','v2_input_manifest') ORDER BY purpose LOOP
  IF photo.v29_replacement_detector_transport_body_ready(p_tenant,p_run,p_owner,d.purpose,d.v29_perspective_lines_transport,p_proof) IS DISTINCT FROM true
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
   AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
   AND a.object_version=d.object_version AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL
   AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
   AND a.output_role=d.purpose AND a.media_type='application/json'
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 END LOOP;
 chunk:=c.v29_perspective_lines_transport::jsonb;manifest:=m.v29_perspective_lines_transport::jsonb;
 RETURN jsonb_build_object('schema_version',1,'task_id',p_owner,'entries',chunk->'entries',
 'entry_count',jsonb_array_length(chunk->'entries'),'entries_sha',manifest->>'entries_sha',
 'chunks',jsonb_build_array(jsonb_build_object('artifact_id',c.artifact_id,'sha256',c.sha256)),
 'manifest',jsonb_build_object('artifact_id',m.artifact_id,'sha256',m.sha256),
 'chunk_jcs',c.v29_perspective_lines_transport,'manifest_jcs',m.v29_perspective_lines_transport);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_replacement_detector_transport_admission(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_replacement_detector_transport_admission(uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.read_v29_replacement_detector_transport_admission(uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
