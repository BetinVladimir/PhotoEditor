-- +goose Up
-- Ready lineage replay must read the exact SQL-pinned immutable version.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_ready_signing_lineage_version(p_tenant uuid,p_actor uuid,p_session uuid,p_export uuid,
 p_expected jsonb,p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;d photo.backend_documents%ROWTYPE;a photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_export IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR p_artifact IS DISTINCT FROM photo.v30_uuid5(p_export,'signing:lineage:'||p_sha)
 OR p_bytes NOT BETWEEN 2 AND 8388608 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
 THEN RETURN NULL;END IF;
 rid:=photo.v31_signing_lineage_scope(p_tenant,p_actor,p_session,p_export,p_expected,p_bucket);
 IF rid IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=rid
  AND owner_id=p_export AND purpose='v31_signing_lineage' AND artifact_id=p_artifact
  AND state='ready' AND sha256=p_sha AND bytes=p_bytes AND bucket=p_bucket
  AND object_version NOT IN('','null') FOR SHARE;
 IF d.artifact_id IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact AND state='ready'
  AND sha256=p_sha AND bytes=p_bytes AND bucket=p_bucket AND object_version=d.object_version
  AND object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||p_artifact
  AND media_type='application/json' AND output_role='v31_signing_lineage' AND verified_at IS NOT NULL
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF a.id IS NULL THEN RETURN NULL;END IF;
 RETURN d.object_version;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v31_ready_signing_lineage_version(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_ready_signing_lineage_version(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v31_ready_signing_lineage_version(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text) TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
