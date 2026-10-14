-- +goose Up
-- The scheduler may enter the generic adoption chain for identity.compare
-- only with the exact four-member physically verified closure. The caller
-- must hold the live source, consent, qualification and object-pin locks in
-- the same transaction before invoking this function.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v23_identity_verified(p_tenant uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text,
 p_calibration uuid,p_calibration_sha text,p_mask uuid,p_mask_sha text,
 p_pixels uuid,p_pixels_sha text) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;root jsonb;t photo.tasks%ROWTYPE;result_row photo.task_results%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM
  nullif(current_setting('app.tenant_id',true),'')::uuid OR
  p_task IS NULL OR p_result IS NULL OR p_calibration IS NULL OR
  p_mask IS NULL OR p_pixels IS NULL OR p_attempt<=0 OR p_fence<=0 OR
  p_digest !~ '^[0-9a-f]{64}$' OR
  p_calibration_sha !~ '^[0-9a-f]{64}$' OR
  p_mask_sha !~ '^[0-9a-f]{64}$' OR p_pixels_sha !~ '^[0-9a-f]{64}$' OR
  p_inventory IS NULL OR jsonb_typeof(p_inventory)<>'array' OR
  jsonb_array_length(p_inventory)<>4 OR
  p_calibration=p_mask OR p_calibration=p_pixels OR p_mask=p_pixels
  THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND
  operation='identity.compare' AND operation_version='1' AND
  resource_class='gpu' AND state='succeeded' AND NOT cancel_requested AND
  attempt=p_attempt AND fencing_token=p_fence AND canonical_result_id=p_result
  FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO result_row FROM photo.task_results WHERE tenant_id=p_tenant AND
  id=p_result AND task_id=p_task AND attempt=p_attempt AND fencing_token=p_fence
  FOR SHARE;
 IF NOT FOUND OR jsonb_typeof(result_row.outputs)<>'array' OR
  jsonb_array_length(result_row.outputs)<>1 THEN RETURN false;END IF;
 root:=result_row.outputs->0;
 IF coalesce(root->>'artifact_id','') !~
   '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' OR
  coalesce(root->>'sha256','') !~ '^[0-9a-f]{64}$' OR
  (root->>'artifact_id')::uuid IN(p_calibration,p_mask,p_pixels)
  THEN RETURN false;END IF;
 expected:=jsonb_build_array(
  root||jsonb_build_object('purpose',encode(sha256(convert_to(
   'tasks/'||p_task||'/$','UTF8')),'hex'),'ordinal',0),
  jsonb_build_object('artifact_id',p_calibration,'sha256',p_calibration_sha,
   'purpose',encode(sha256(convert_to('tasks/'||p_task||'/$/calibration','UTF8')),'hex'),'ordinal',1),
  jsonb_build_object('artifact_id',p_mask,'sha256',p_mask_sha,
   'purpose',encode(sha256(convert_to('tasks/'||p_task||'/$/changed_mask','UTF8')),'hex'),'ordinal',2),
  jsonb_build_object('artifact_id',p_pixels,'sha256',p_pixels_sha,
   'purpose',encode(sha256(convert_to('tasks/'||p_task||'/$/changed_mask/artifact','UTF8')),'hex'),'ordinal',3));
 IF p_inventory IS DISTINCT FROM expected OR
  NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='Calibration' AND
   i.artifact_id=p_calibration AND i.sha256=p_calibration_sha)
  THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_pre_v23_identity(p_tenant,p_task,p_attempt,p_fence,
  p_result,p_inventory,p_digest);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR
 invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_v23_identity_verified(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,uuid,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v23_identity_verified(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.adopt_v23_identity_verified(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,uuid,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
