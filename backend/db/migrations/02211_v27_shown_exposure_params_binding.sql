-- +goose Up
-- Pure binding only: no ACL, live lease, qualification or approval authority.
-- Atomic admission must obtain receipt/preview Params from locked canonical rows,
-- never substitute client supplied JSON as evidence of photographer acceptance.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_exposure_params_bound(p_tenant uuid,p_child uuid,p_target uuid,
 p_preview uuid,p_approved jsonb,p_params jsonb,p_preview_params jsonb,p_receipt jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE k text;v jsonb;ids uuid[]:=ARRAY[]::uuid[];aid uuid;item text;
BEGIN
 IF jsonb_typeof(p_params) IS DISTINCT FROM 'object'
 OR NOT(p_params ?& ARRAY['schema_version','tenant_id','child_run_id','target_id','approved_edit',
 'preview_run_id','preview_task_id','preview_result_id','preview_attempt','preview_fence',
 'presentation','numerical_evidence','working_image','pixels','model_lock_sha','runtime_lock_sha','device_profile_sha'])
 OR (p_params-ARRAY['schema_version','tenant_id','child_run_id','target_id','approved_edit',
 'preview_run_id','preview_task_id','preview_result_id','preview_attempt','preview_fence',
 'presentation','numerical_evidence','working_image','pixels','model_lock_sha','runtime_lock_sha','device_profile_sha'])<>'{}'::jsonb
 OR p_params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_params->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR p_params->>'child_run_id' IS DISTINCT FROM p_child::text
 OR p_params->>'target_id' IS DISTINCT FROM p_target::text
 OR p_params->>'preview_run_id' IS DISTINCT FROM p_preview::text
 OR p_params->'approved_edit' IS DISTINCT FROM p_approved
 OR p_preview_params->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR p_preview_params->>'target_id' IS DISTINCT FROM p_target::text
 OR p_receipt->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN false;END IF;
 FOREACH k IN ARRAY ARRAY['tenant_id','child_run_id','target_id','preview_run_id','preview_task_id','preview_result_id'] LOOP
  IF jsonb_typeof(p_params->k) IS DISTINCT FROM 'string'
  OR coalesce(p_params->>k,'') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN RETURN false;END IF;
  aid:=(p_params->>k)::uuid;
  IF aid='00000000-0000-0000-0000-000000000000'::uuid OR aid=ANY(ids) THEN RETURN false;END IF;
  ids:=array_append(ids,aid);
 END LOOP;
 FOREACH k IN ARRAY ARRAY['approved_edit','presentation','numerical_evidence','working_image','pixels'] LOOP
  v:=p_params->k;
  IF jsonb_typeof(v) IS DISTINCT FROM 'object' OR NOT(v ?& ARRAY['artifact_id','sha256'])
  OR (v-'artifact_id'-'sha256')<>'{}'::jsonb
  OR jsonb_typeof(v->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(v->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(v->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(v->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(v->>'artifact_id')::uuid;
  IF aid='00000000-0000-0000-0000-000000000000'::uuid OR aid=ANY(ids) THEN RETURN false;END IF;
  ids:=array_append(ids,aid);
  IF k<>'approved_edit' AND v IS DISTINCT FROM p_receipt->k THEN RETURN false;END IF;
 END LOOP;
 FOREACH k IN ARRAY ARRAY['model_lock_sha','runtime_lock_sha','device_profile_sha'] LOOP
  IF jsonb_typeof(p_params->k) IS DISTINCT FROM 'string' OR coalesce(p_params->>k,'') !~ '^[0-9a-f]{64}$'
  OR p_params->k IS DISTINCT FROM p_preview_params->k THEN RETURN false;END IF;
 END LOOP;
 FOREACH k IN ARRAY ARRAY['preview_attempt','preview_fence'] LOOP
  item:=p_params->>k;
  IF jsonb_typeof(p_params->k) IS DISTINCT FROM 'number' OR coalesce(item,'') !~ '^[1-9][0-9]{0,18}$'
  OR item::numeric>(CASE WHEN k='preview_attempt' THEN 2147483647::numeric ELSE 9223372036854775807::numeric END)
  OR p_params->k IS DISTINCT FROM p_receipt->replace(k,'preview_','') THEN RETURN false;END IF;
 END LOOP;
 FOREACH k IN ARRAY ARRAY['plan','intent','candidate','protection'] LOOP
  IF p_approved->>'artifact_id' IS NOT DISTINCT FROM p_preview_params->k->>'artifact_id' THEN RETURN false;END IF;
 END LOOP;
 IF p_approved->>'artifact_id' IS NOT DISTINCT FROM p_preview_params->'source'->'source'->>'artifact_id' THEN RETURN false;END IF;
 RETURN coalesce(p_params->>'preview_task_id'=photo.semantic_uuid_v5(p_preview,'semantic.preview_local_exposure:'||p_target::text)::text
 AND p_params->'preview_task_id'=p_receipt->'task_id'
 AND p_params->'preview_result_id'=p_receipt->'result_id',false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_exposure_params_bound(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_exposure_params_bound(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
