-- +goose Up
-- Preserve safe numeric copy documents; accept lossless high decimal-string fences.
-- Do not rewrite persisted artifact bytes or SHA bindings.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_shown_removal_params_bound(p_tenant uuid,p_child uuid,p_target uuid,
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
 item:=p_params->>'preview_attempt';
 IF jsonb_typeof(p_params->'preview_attempt') IS DISTINCT FROM 'number'
 OR coalesce(item,'') !~ '^[1-9][0-9]{0,9}$' THEN RETURN false;END IF;
 IF item::numeric>2147483647::numeric
 OR p_params->'preview_attempt' IS DISTINCT FROM p_receipt->'attempt' THEN RETURN false;END IF;
 IF photo.shown_copy_fence_wire_value(p_params->'preview_fence') IS NULL
 OR photo.shown_copy_fence_wire_value(p_params->'preview_fence')::text
    IS DISTINCT FROM p_receipt->>'fence' THEN RETURN false;END IF;
 FOREACH k IN ARRAY ARRAY['plan','intent','candidate','protection'] LOOP
  IF p_approved->>'artifact_id' IS NOT DISTINCT FROM p_preview_params->k->>'artifact_id' THEN RETURN false;END IF;
 END LOOP;
 IF p_approved->>'artifact_id' IS NOT DISTINCT FROM p_preview_params->'source'->'source'->>'artifact_id' THEN RETURN false;END IF;
 RETURN coalesce(p_params->>'preview_task_id'=photo.semantic_uuid_v5(p_preview,'semantic.preview_remove_object:'||p_target::text)::text
 AND p_params->'preview_task_id'=p_receipt->'task_id'
 AND p_params->'preview_result_id'=p_receipt->'result_id',false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_shown_retouch_params_bound(p_tenant uuid,p_child uuid,p_target uuid,
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
 item:=p_params->>'preview_attempt';
 IF jsonb_typeof(p_params->'preview_attempt') IS DISTINCT FROM 'number'
 OR coalesce(item,'') !~ '^[1-9][0-9]{0,9}$' THEN RETURN false;END IF;
 IF item::numeric>2147483647::numeric
 OR p_params->'preview_attempt' IS DISTINCT FROM p_receipt->'attempt' THEN RETURN false;END IF;
 IF photo.shown_copy_fence_wire_value(p_params->'preview_fence') IS NULL
 OR photo.shown_copy_fence_wire_value(p_params->'preview_fence')::text
    IS DISTINCT FROM p_receipt->>'fence' THEN RETURN false;END IF;
 FOREACH k IN ARRAY ARRAY['plan','intent','candidate','protection'] LOOP
  IF p_approved->>'artifact_id' IS NOT DISTINCT FROM p_preview_params->k->>'artifact_id' THEN RETURN false;END IF;
 END LOOP;
 IF p_approved->>'artifact_id' IS NOT DISTINCT FROM p_preview_params->'source'->'source'->>'artifact_id' THEN RETURN false;END IF;
 RETURN coalesce(p_params->>'preview_task_id'=photo.semantic_uuid_v5(p_preview,'semantic.preview_retouch_region:'||p_target::text)::text
 AND p_params->'preview_task_id'=p_receipt->'task_id'
 AND p_params->'preview_result_id'=p_receipt->'result_id',false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
