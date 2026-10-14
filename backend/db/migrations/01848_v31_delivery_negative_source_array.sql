-- +goose Up
-- Exact registered source NPY input for a delegated negative task.
-- The caller derives RGB/alpha/inverse-field role and shape from physically
-- verified source WorkingImage bytes and repeats the lookup after streaming.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_negative_source_array(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,p_task uuid,
 p_source uuid,p_source_sha text,p_array uuid,p_array_sha text,
 p_bucket text,p_min_bytes bigint)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;result jsonb;
BEGIN
 IF p_source_sha!~'^[0-9a-f]{64}$' OR p_array_sha!~'^[0-9a-f]{64}$'
  OR p_min_bytes<4 OR length(p_bucket)<3 OR length(p_bucket)>63
  THEN RETURN NULL;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'optical.negative' OR e->>'version'<>'1'
  THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('bucket',a.bucket,'key',a.object_key,
  'version',a.object_version,'sha256',a.sha256,'bytes',a.bytes,
  'media_type',a.media_type)
 INTO result FROM photo.v2_task_inputs i
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id
  AND a.id=i.artifact_id AND a.sha256=i.sha256
 JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id
  AND w.run_id=(e->>'run')::uuid AND w.project_id=i.project_id
 WHERE i.tenant_id=(source->>'tenant_id')::uuid
  AND i.task_id=p_task AND i.project_id=(source->>'project_id')::uuid
  AND i.contract='NpyArray' AND i.artifact_id=p_array AND i.sha256=p_array_sha
  AND w.workflow_type='preview' AND w.renderer_version='v2.2'
  AND a.bucket=p_bucket AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null') AND a.media_type='application/x-npy'
  AND a.bytes BETWEEN p_min_bytes AND p_min_bytes+65536
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.v2_task_inputs parent
   WHERE parent.tenant_id=i.tenant_id AND parent.task_id=i.task_id
    AND parent.project_id=i.project_id AND parent.contract='WorkingImage'
    AND parent.artifact_id=p_source AND parent.sha256=p_source_sha)
 FOR SHARE OF a;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_negative_source_array(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_negative_source_array(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_negative_source_array(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text,bigint)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
