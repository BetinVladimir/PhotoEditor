-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_dust_sensor_metadata(p_tenant uuid,p_workflow uuid,p_parsed text,p_version uuid,p_master uuid,p_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;metadata jsonb;result jsonb;matches integer;
BEGIN
 IF p_version IS NULL OR p_master IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL THEN RETURN NULL;END IF;
 metadata:=photo.v30_interchange_metadata_receipts(p_tenant,p_workflow,p_parsed);
 IF metadata IS NULL THEN RETURN NULL;END IF;
 SELECT count(*) INTO matches FROM jsonb_array_elements(sources->'entries') e
 WHERE e->>'state'='adopted' AND e->>'asset_version_id'=p_version::text
 AND e->'master'=jsonb_build_object('artifact_id',p_master,'sha256',p_sha);
 IF matches<>1 THEN RETURN NULL;END IF;
 SELECT v.metadata->'ingest'->'sensor_alignment' INTO result
 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 WHERE v.tenant_id=p_tenant AND v.id=p_version AND v.master_artifact_id=p_master AND v.content_hash=p_sha
 AND a.project_id=(sources->>'project_id')::uuid AND a.deleted_at IS NULL AND a.upload_state='verified'
 AND v.review_state='accepted' AND v.metadata#>>'{ingest,format}'='raw'
 AND jsonb_typeof(v.metadata->'ingest'->'sensor_alignment')='object';
 IF result IS NULL OR (SELECT count(*) FROM jsonb_object_keys(result))<>8
 OR NOT(result ?& ARRAY['camera_id','sensor_width','sensor_height','crop_x','crop_y','crop_width','crop_height','orientation'])
 OR result->>'camera_id' !~ '^raw-[0-9a-f]{64}$' OR (result->>'orientation')::integer<>1
 OR (result->>'sensor_width')::bigint NOT BETWEEN 8 AND 200000
 OR (result->>'sensor_height')::bigint NOT BETWEEN 8 AND 200000
 OR (result->>'crop_width')::bigint NOT BETWEEN 8 AND 200000
 OR (result->>'crop_height')::bigint NOT BETWEEN 8 AND 200000
 OR (result->>'crop_x')::bigint<0 OR (result->>'crop_y')::bigint<0
 OR (result->>'crop_x')::bigint+(result->>'crop_width')::bigint>(result->>'sensor_width')::bigint
 OR (result->>'crop_y')::bigint+(result->>'crop_height')::bigint>(result->>'sensor_height')::bigint THEN RETURN NULL;END IF;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_import_dust_sensor_metadata(uuid,uuid,text,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_dust_sensor_metadata(uuid,uuid,text,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_dust_sensor_metadata(uuid,uuid,text,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
