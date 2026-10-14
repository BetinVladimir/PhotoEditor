-- +goose Up
-- Session-free original-byte authority for a delegated asset.decode ancestor.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_original_source(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,p_task uuid,
 p_original uuid,p_original_sha text,p_master uuid,p_master_sha text,p_bucket text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;result jsonb;
BEGIN
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL OR p_original IS NULL OR p_master IS NULL OR
  p_original=p_master OR p_original_sha!~'^[0-9a-f]{64}$' OR
  p_master_sha!~'^[0-9a-f]{64}$' OR
  length(p_bucket)<3 OR length(p_bucket)>63 THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'asset.decode' OR e->>'version'<>'1'
  OR e->'inputs'<>jsonb_build_array(jsonb_build_object(
   'artifact_id',p_original::text,'sha256',p_original_sha))
  OR jsonb_array_length(e->'outputs') NOT BETWEEN 2 AND 3
  OR e->'outputs'->(jsonb_array_length(e->'outputs')-1)<>
   jsonb_build_object('artifact_id',p_master::text,'sha256',p_master_sha)
  THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('bucket',original.bucket,'key',original.object_key,
  'version',original.object_version,'sha256',original.sha256,
  'bytes',original.bytes,'media_type',original.media_type)
 INTO result FROM photo.assets asset
 JOIN photo.asset_versions version ON version.tenant_id=asset.tenant_id
  AND version.asset_id=asset.id
 JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id
  AND original.id=asset.original_artifact_id
 JOIN photo.artifacts working ON working.tenant_id=version.tenant_id
  AND working.id=version.master_artifact_id
 WHERE asset.tenant_id=(source->>'tenant_id')::uuid
  AND asset.project_id=(source->>'project_id')::uuid
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
  AND original.id=p_original AND original.sha256=p_original_sha
  AND original.bucket=p_bucket AND original.state='ready'
  AND original.verified_at IS NOT NULL
  AND original.object_version NOT IN('','null')
  AND original.bytes BETWEEN 1 AND 536870912
  AND original.media_type<>'application/json'
  AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
  AND version.master_artifact_id=p_master
  AND version.content_hash=p_master_sha
  AND version.created_by_run_id=(e->>'run')::uuid
  AND working.sha256=p_master_sha AND working.created_by_task_id=p_task
  AND working.produced_attempt=(e->>'attempt')::integer
  AND working.produced_fence=(e->>'fence')::bigint
  AND working.state='ready' AND working.verified_at IS NOT NULL
  AND working.object_version NOT IN('','null')
  AND working.media_type='application/json'
  AND working.bytes BETWEEN 2 AND 1048576
  AND (working.expires_at IS NULL OR working.expires_at>clock_timestamp())
 ORDER BY version.id LIMIT 1 FOR SHARE OF asset,version,original,working;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_original_source(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_original_source(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_original_source(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
