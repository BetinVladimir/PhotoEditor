-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_source_document(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb;
BEGIN
 IF photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,
 'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) INTO result
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(p_base->'source'->>'artifact_id')::uuid
 AND a.sha256=p_base->'source'->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 65536
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_perspective_source_document(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_source_document(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_source_document(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
