-- +goose Up
-- Exact parsed file membership grants opaque byte reads only, never typed document approval.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_import_file(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;body jsonb;
BEGIN
 IF p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR photo.v30_interchange_metadata_receipts(p_tenant,p_workflow,p_parsed) IS NULL THEN RETURN;END IF;
 body:=p_parsed::jsonb;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(body->'files') AS f(value)
  WHERE value->'content'=jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha)) THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.state='ready'
 AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null') AND a.media_type='application/octet-stream'
 AND a.bytes BETWEEN 1 AND 137438953472 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
END $$;
ALTER FUNCTION photo.v30_interchange_import_file(uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_file(uuid,uuid,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_import_file(uuid,uuid,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
