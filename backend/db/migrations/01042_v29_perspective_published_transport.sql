-- +goose Up
-- Read exact published transport. Does not register task inputs or admit work.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_perspective_published_transport(p_tenant uuid,p_run uuid,p_task uuid,p_descriptor text,p_proof jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE descriptor text;entries jsonb;entry_text text;entry_sha text;chunk_body text;manifest_body text;
 chunk photo.backend_documents%ROWTYPE;manifest photo.backend_documents%ROWTYPE;refs jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT v29_perspective_descriptor_jcs INTO descriptor FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF descriptor IS NULL OR descriptor IS DISTINCT FROM p_descriptor OR descriptor::jsonb->'task'->>'id' IS DISTINCT FROM p_task::text THEN RETURN NULL;END IF;
 -- Stage's replay validator must not manufacture a missing task or reservation.
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task AND state='blocked' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run AND state='reserved' AND expires_at>clock_timestamp() FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 entries:=descriptor::jsonb->'entries';
 entry_text:=photo.v27_semantic_policy_json(entries);entry_sha:=encode(sha256(convert_to(entry_text,'UTF8')),'hex');
 SELECT * INTO chunk FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task AND purpose='v2_input_chunk_0' AND state='ready' FOR SHARE;
 SELECT * INTO manifest FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task AND purpose='v2_input_manifest' AND state='ready' FOR SHARE;
 IF chunk.id IS NULL OR manifest.id IS NULL OR chunk.artifact_id=manifest.artifact_id THEN RETURN NULL;END IF;
 chunk_body:='{"entries":'||entry_text||',"schema_version":1}';
 refs:=jsonb_build_array(jsonb_build_object('artifact_id',chunk.artifact_id,'sha256',chunk.sha256));
 manifest_body:=photo.v27_semantic_policy_json(jsonb_build_object('schema_version',1,'task_id',p_task,'entry_count',jsonb_array_length(entries),'entries_sha',entry_sha,'chunks',refs));
 IF photo.v29_perspective_transport_document_scope(p_tenant,p_run,p_task,'v2_input_chunk_0',chunk.sha256,chunk.bytes,p_proof,chunk_body) IS DISTINCT FROM true
 OR photo.v29_perspective_transport_document_scope(p_tenant,p_run,p_task,'v2_input_manifest',manifest.sha256,manifest.bytes,p_proof,manifest_body) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM (VALUES(chunk.artifact_id,chunk.sha256,chunk.bytes,chunk.bucket,chunk.object_version,chunk.purpose),
 (manifest.artifact_id,manifest.sha256,manifest.bytes,manifest.bucket,manifest.object_version,manifest.purpose)) d(id,sha,bytes,bucket,version,purpose)
 WHERE NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.id AND a.sha256=d.sha AND a.bytes=d.bytes
 AND a.bucket=d.bucket AND a.object_version=d.version AND a.object_version NOT IN('','null') AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.output_role=d.purpose AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a)) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('manifest',jsonb_build_object('artifact_id',manifest.artifact_id,'sha256',manifest.sha256),'chunks',refs,'count',jsonb_array_length(entries),'sha256',entry_sha);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_perspective_published_transport(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_perspective_published_transport(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_perspective_published_transport(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
