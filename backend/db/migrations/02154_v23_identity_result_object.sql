-- +goose Up
-- Resolve only ready outputs of one canonical identity.compare result. The
-- caller verifies document-child links and physical NPY bytes before adoption.
-- +goose StatementBegin
CREATE FUNCTION photo.v23_identity_result_object(p_tenant uuid,p_task uuid,
 p_result uuid,p_root uuid,p_root_sha text,p_role text,p_artifact uuid,
 p_sha text,p_bucket text) RETURNS jsonb
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task_row photo.tasks%ROWTYPE;result_row photo.task_results%ROWTYPE;
 root_row photo.artifacts%ROWTYPE;member photo.artifacts%ROWTYPE;
 expected_role text;expected_media text;max_bytes bigint;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM
   nullif(current_setting('app.tenant_id',true),'')::uuid OR
  p_task IS NULL OR p_result IS NULL OR p_root IS NULL OR p_artifact IS NULL OR
  p_root_sha IS NULL OR p_sha IS NULL OR p_bucket IS NULL OR p_role IS NULL OR
  p_root_sha !~ '^[0-9a-f]{64}$' OR p_sha !~ '^[0-9a-f]{64}$' OR
  length(p_bucket) NOT BETWEEN 3 AND 63 OR
  p_role NOT IN('report','mask','pixels') THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task
  AND operation='identity.compare' AND operation_version='1' AND
  resource_class='gpu' AND state='succeeded' AND NOT cancel_requested AND
  canonical_result_id=p_result AND attempt>0 AND fencing_token>0 FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO result_row FROM photo.task_results WHERE tenant_id=p_tenant
  AND id=p_result AND task_id=p_task AND attempt=task_row.attempt AND
  fencing_token=task_row.fencing_token FOR SHARE;
 IF NOT FOUND OR result_row.outputs IS DISTINCT FROM
  jsonb_build_array(jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha))
  THEN RETURN NULL;END IF;
 SELECT * INTO root_row FROM photo.artifacts WHERE tenant_id=p_tenant
  AND id=p_root AND sha256=p_root_sha AND bucket=p_bucket AND
  output_role='document' AND media_type='application/json' AND
  created_by_task_id=p_task AND produced_attempt=task_row.attempt AND
  produced_fence=task_row.fencing_token AND state='ready' AND
  verified_at IS NOT NULL AND object_version IS NOT NULL AND
  object_version NOT IN('','null') AND
  (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF p_role='report' AND
  (p_artifact IS DISTINCT FROM p_root OR p_sha IS DISTINCT FROM p_root_sha)
  THEN RETURN NULL;END IF;
 expected_role:=CASE p_role WHEN 'report' THEN 'document'
  WHEN 'mask' THEN 'mask' ELSE 'mask_pixels' END;
 expected_media:=CASE p_role WHEN 'pixels' THEN 'application/x-npy'
  ELSE 'application/json' END;
 max_bytes:=CASE p_role WHEN 'pixels' THEN 800004096 ELSE 8388608 END;
 SELECT * INTO member FROM photo.artifacts WHERE tenant_id=p_tenant
  AND id=p_artifact AND sha256=p_sha AND bucket=p_bucket AND
  created_by_task_id=p_task AND produced_attempt=task_row.attempt AND
  produced_fence=task_row.fencing_token AND output_role=expected_role AND
  media_type=expected_media AND bytes BETWEEN 2 AND max_bytes AND
  state='ready' AND verified_at IS NOT NULL AND object_version IS NOT NULL
  AND object_version NOT IN('','null') AND
  (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND OR (p_role='pixels' AND member.bytes<14) THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.output_uploads u
  WHERE u.tenant_id=p_tenant AND u.task_id=p_task AND
   u.attempt=task_row.attempt AND u.fence=task_row.fencing_token AND
   u.artifact_id=member.id AND u.role=expected_role AND u.state='ready' AND
   u.bucket=member.bucket AND u.object_key=member.object_key AND
   u.object_version=member.object_version AND u.sha256=member.sha256 AND
   u.bytes=member.bytes AND u.media_type=member.media_type)
  THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('key',member.object_key,
  'version',member.object_version,'sha256',member.sha256,
  'bytes',member.bytes,'media_type',member.media_type);
END $$;
ALTER FUNCTION photo.v23_identity_result_object(uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_identity_result_object(uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v23_identity_result_object(uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
