-- +goose Up
-- Private metadata gate. Caller independently verifies the actual canonical
-- chunk/manifest bytes and approved child/task namespace before registration.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_transport_ready(p_tenant uuid,p_parent uuid,p_task uuid,p_count integer,p_manifest jsonb,p_chunks jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE refs jsonb; item jsonb; transport_purpose text; ordinal integer:=0;
BEGIN
 IF p_tenant IS NULL OR p_parent IS NULL OR p_task IS NULL OR p_task='00000000-0000-0000-0000-000000000000'::uuid
 OR p_count IS NULL OR p_count NOT BETWEEN 1 AND 1000000
 OR jsonb_typeof(p_manifest) IS DISTINCT FROM 'object' OR jsonb_typeof(p_chunks) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_chunks)<>(p_count+511)/512 THEN RETURN false;END IF;
 refs:=p_chunks||jsonb_build_array(p_manifest);
 IF (SELECT count(DISTINCT r->>'artifact_id') FROM jsonb_array_elements(refs) r)<>jsonb_array_length(refs) THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant
 AND id IN(SELECT (r->>'artifact_id')::uuid FROM jsonb_array_elements(refs) r) ORDER BY id FOR SHARE;
 PERFORM id FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_parent AND owner_id=p_task
 AND artifact_id IN(SELECT (r->>'artifact_id')::uuid FROM jsonb_array_elements(refs) r) ORDER BY id FOR SHARE;
 FOR item IN SELECT value FROM jsonb_array_elements(refs) LOOP
  transport_purpose:=CASE WHEN ordinal=jsonb_array_length(p_chunks) THEN 'v2_input_manifest' ELSE 'v2_input_chunk_'||ordinal END;
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR NOT(item ?& ARRAY['artifact_id','sha256'])
  OR (item-'artifact_id'-'sha256')<>'{}'::jsonb OR jsonb_typeof(item->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(item->'sha256') IS DISTINCT FROM 'string' OR coalesce(item->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR (item->>'artifact_id')::uuid IS DISTINCT FROM photo.semantic_uuid_v5(p_task,transport_purpose) THEN RETURN false;END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
   WHERE d.tenant_id=p_tenant AND d.run_id=p_parent AND d.owner_id=p_task AND d.purpose=transport_purpose
   AND d.artifact_id=(item->>'artifact_id')::uuid AND d.sha256=item->>'sha256' AND d.state='ready'
   AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bytes BETWEEN 2 AND 1048576
   AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND a.object_key='tenants/'||p_tenant||'/runs/'||p_parent||'/documents/'||a.id
   AND a.media_type='application/json' AND a.output_role=transport_purpose AND a.verified_at IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
  ordinal:=ordinal+1;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_approved_transport_ready(uuid,uuid,uuid,integer,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_transport_ready(uuid,uuid,uuid,integer,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
