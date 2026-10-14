-- +goose Up
-- Pins only exact current canonical skin outputs and accepted-version Params.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_skin_result_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid,p_result uuid)
RETURNS TABLE(ordinal integer,artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE found_result record;refs jsonb;ref jsonb;i integer;
BEGIN
 SELECT * INTO found_result FROM photo.semantic_native_skin_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task);
 IF NOT FOUND OR found_result.result_id IS DISTINCT FROM p_result THEN RETURN;END IF;
 refs:=found_result.outputs||jsonb_build_array(found_result.params);
 FOR i IN 0..3 LOOP
  ref:=refs->i;
  RETURN QUERY SELECT i,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
  FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.bytes>0 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 END LOOP;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.semantic_native_skin_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_skin_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_skin_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
