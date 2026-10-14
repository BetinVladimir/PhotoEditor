-- +goose Up
-- Read only the immutable Params and after WorkingImage registered for the
-- same canonical identity.compare task. Go derives after from exact Params.
-- +goose StatementBegin
CREATE FUNCTION photo.v23_identity_result_input(p_tenant uuid,p_task uuid,
 p_result uuid,p_root uuid,p_root_sha text,p_role text,p_artifact uuid,
 p_sha text,p_bucket text) RETURNS jsonb
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task_row photo.tasks%ROWTYPE;member photo.artifacts%ROWTYPE;
 project uuid;expected_contract text;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM
   nullif(current_setting('app.tenant_id',true),'')::uuid OR
  p_task IS NULL OR p_result IS NULL OR p_root IS NULL OR p_artifact IS NULL OR
  p_root_sha IS NULL OR p_sha IS NULL OR p_bucket IS NULL OR p_role IS NULL OR
  p_root_sha !~ '^[0-9a-f]{64}$' OR p_sha !~ '^[0-9a-f]{64}$' OR
  length(p_bucket) NOT BETWEEN 3 AND 63 OR p_role NOT IN('params','after')
  THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task
  AND operation='identity.compare' AND operation_version='1' AND
  state='succeeded' AND NOT cancel_requested AND
  canonical_result_id=p_result AND attempt>0 AND fencing_token>0 FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT w.project_id INTO project FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.run_id=task_row.run_id;
 IF project IS NULL OR NOT EXISTS(SELECT 1 FROM photo.task_results z
  WHERE z.tenant_id=p_tenant AND z.id=p_result AND z.task_id=p_task AND
   z.attempt=task_row.attempt AND z.fencing_token=task_row.fencing_token AND
   z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',p_root,
    'sha256',p_root_sha))) THEN RETURN NULL;END IF;
 IF p_role='params' AND p_artifact IS DISTINCT FROM task_row.params_artifact_id
  THEN RETURN NULL;END IF;
 expected_contract:=CASE p_role WHEN 'params' THEN 'Params' ELSE 'WorkingImage' END;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
  WHERE i.tenant_id=p_tenant AND i.project_id=project AND i.task_id=p_task
   AND i.contract=expected_contract AND i.artifact_id=p_artifact AND
   i.sha256=p_sha) THEN RETURN NULL;END IF;
 SELECT * INTO member FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND
  a.id=p_artifact AND a.sha256=p_sha AND a.bucket=p_bucket AND
  a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576 AND
  a.state='ready' AND a.verified_at IS NOT NULL AND
  a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND
  (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND OR (p_role='after' AND member.output_role<>'working_image')
  THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('key',member.object_key,
  'version',member.object_version,'sha256',member.sha256,
  'bytes',member.bytes,'media_type',member.media_type);
END $$;
ALTER FUNCTION photo.v23_identity_result_input(uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_identity_result_input(uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v23_identity_result_input(uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
