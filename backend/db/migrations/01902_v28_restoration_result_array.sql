-- +goose Up
-- Locate only versioned NPY members of the exact adopted restoration result.
-- The Go verifier proves document-child references and physical pixel parity.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_result_array(
 p_tenant uuid,p_task uuid,p_result uuid,p_root uuid,p_root_sha text,
 p_role text,p_artifact uuid,p_sha text,p_bucket text)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;root photo.artifacts%ROWTYPE;
 member photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_task IS NULL OR p_result IS NULL OR p_root IS NULL
 OR p_artifact IS NULL OR p_role IS NULL OR p_bucket IS NULL
 OR p_root_sha IS NULL OR p_sha IS NULL
 OR p_root_sha!~'^[0-9a-f]{64}$' OR p_sha!~'^[0-9a-f]{64}$'
 OR length(p_bucket) NOT BETWEEN 3 AND 63
 OR p_role NOT IN('source_pixels','output_pixels','target_pixels',
  'protected_pixels','changed_pixels','generated_pixels') THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task
  AND operation IN('portrait.glare','image.reflection') AND operation_version='1'
  AND state='succeeded' AND NOT cancel_requested AND canonical_result_id=p_result
  AND attempt>0 AND fencing_token>0 FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result
  AND task_id=p_task AND attempt=t.attempt AND fencing_token=t.fencing_token
  AND review_required FOR SHARE;
 IF NOT FOUND OR z.outputs IS DISTINCT FROM jsonb_build_array(
  jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha)) THEN RETURN NULL;END IF;
 SELECT * INTO root FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_root
  AND sha256=p_root_sha AND bucket=p_bucket AND state='ready'
  AND verified_at IS NOT NULL AND object_version IS NOT NULL
  AND object_version NOT IN('','null') AND media_type='application/json'
  AND output_role='document' AND created_by_task_id=p_task
  AND produced_attempt=t.attempt AND produced_fence=t.fencing_token
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO member FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact
  AND sha256=p_sha AND bucket=p_bucket AND state='ready'
  AND verified_at IS NOT NULL AND object_version IS NOT NULL
  AND object_version NOT IN('','null') AND media_type='application/x-npy'
  AND bytes BETWEEN 14 AND 2400065536
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF p_role IN('source_pixels','target_pixels','protected_pixels') THEN
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.artifact_id=p_artifact AND i.sha256=p_sha
   AND i.contract='NpyArray')
  THEN RETURN NULL;END IF;
 ELSE
  IF member.created_by_task_id IS DISTINCT FROM p_task
   OR member.produced_attempt IS DISTINCT FROM t.attempt
   OR member.produced_fence IS DISTINCT FROM t.fencing_token
   OR member.output_role IS DISTINCT FROM (CASE p_role WHEN 'output_pixels' THEN 'pixels'
    ELSE 'mask_pixels' END) THEN RETURN NULL;END IF;
 END IF;
 RETURN jsonb_build_object('key',member.object_key,'version',member.object_version,
  'sha256',member.sha256,'bytes',member.bytes,'media_type',member.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value
 THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_result_array(uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_result_array(uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_result_array(uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
