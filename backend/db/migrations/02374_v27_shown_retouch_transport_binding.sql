-- +goose Up
-- Separate removal copy admission; does not enable or materialize tasks.
-- Independent JCS reconstruction for the shown-result adoption inventory.
-- Allowed contract names, UUIDs and SHA strings are ASCII, so this exact wire
-- grammar requires no general JSON canonicalizer. No authority/admission here.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_retouch_inventory_digest_ready(p_task uuid,p_params uuid,p_params_sha text,p_input_digest text,p_entries_sha text,p_entries jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; ref jsonb; aid uuid; prior uuid; contract_name text; body text:='['; first_item boolean:=true; params_count integer:=0;
BEGIN
 IF p_task IS NULL OR p_task='00000000-0000-0000-0000-000000000000'::uuid OR p_params IS NULL
 OR p_params='00000000-0000-0000-0000-000000000000'::uuid
 OR coalesce(p_params_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_input_digest,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_entries_sha,'') !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_entries) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_entries) NOT BETWEEN 1 AND 512 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_entries) LOOP
  ref:=item->'ref';contract_name:=item->>'contract';
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR NOT(item ?& ARRAY['ref','contract'])
  OR (item-'ref'-'contract')<>'{}'::jsonb OR jsonb_typeof(item->'contract') IS DISTINCT FROM 'string'
  OR contract_name NOT IN('Params','SemanticPlan','WorkingImage','EditIntent','TransferCandidate','MaskRef','ResolvedTransform','NpyArray','ApprovedSemanticEdit','RetouchPreviewPresentation','RetouchPreviewNumericalEvidence','EncodedImage')
  OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT(ref ?& ARRAY['artifact_id','sha256'])
  OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF aid='00000000-0000-0000-0000-000000000000'::uuid OR (prior IS NOT NULL AND aid<=prior) THEN RETURN false;END IF;
  IF contract_name='Params' THEN
   params_count:=params_count+1;
   IF aid<>p_params OR ref->>'sha256'<>p_params_sha THEN RETURN false;END IF;
  ELSIF aid=p_params THEN RETURN false;END IF;
  IF NOT first_item THEN body:=body||',';END IF;
  body:=body||'{"contract":"'||contract_name||'","ref":{"artifact_id":"'||aid::text||'","sha256":"'||(ref->>'sha256')||'"}}';
  first_item:=false;prior:=aid;
 END LOOP;
 body:=body||']';
 RETURN params_count=1 AND encode(sha256(convert_to(body,'UTF8')),'hex')=p_entries_sha
 AND encode(sha256(convert_to('{"entries":'||body||',"operation":"semantic.adopt_shown_retouch_region","task":"'||p_task::text||'","version":"1"}','UTF8')),'hex')=p_input_digest;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_retouch_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_retouch_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Pure binding only: no ACL, live lease, qualification or approval authority.
-- Atomic admission must obtain receipt/preview Params from locked canonical rows,
-- never substitute client supplied JSON as evidence of photographer acceptance.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_retouch_params_bound(p_tenant uuid,p_child uuid,p_target uuid,
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
 RETURN coalesce(p_params->>'preview_task_id'=photo.semantic_uuid_v5(p_preview,'semantic.preview_retouch_region:'||p_target::text)::text
 AND p_params->'preview_task_id'=p_receipt->'task_id'
 AND p_params->'preview_result_id'=p_receipt->'result_id',false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_retouch_params_bound(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_retouch_params_bound(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Pure bounded transport reconstruction; never grants source or execution authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_retouch_transport_ready(p_child uuid,p_params jsonb,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task uuid;params uuid;entries jsonb;roots jsonb;entry_body text;chunk jsonb;manifest jsonb;
 chunk_sha text;manifest_sha text;required record;n integer;
BEGIN
 IF p_child IS NULL OR p_child='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(p_params) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_node) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_transport) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'object'
 OR p_params->>'child_run_id' IS DISTINCT FROM p_child::text
 OR jsonb_typeof(p_params->'target_id') IS DISTINCT FROM 'string'
 OR coalesce(p_params->>'target_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR p_params->>'target_id'='00000000-0000-0000-0000-000000000000' THEN RETURN false;END IF;
 IF NOT(p_node ?& ARRAY['id','node_key','operation','operation_version','resource_class','params_artifact_id','params_sha','input_digest','execution_profile'])
 OR (p_node-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'params_artifact_id'-'params_sha'-'input_digest'-'execution_profile')<>'{}'::jsonb
 OR p_node->>'operation' IS DISTINCT FROM 'semantic.adopt_shown_retouch_region'
 OR p_node->>'operation_version' IS DISTINCT FROM '1' OR p_node->>'resource_class' IS DISTINCT FROM 'cpu'
 OR p_node->>'node_key' IS DISTINCT FROM 'semantic.adopt_shown_retouch_region:'||(p_params->>'target_id')::uuid::text THEN RETURN false;END IF;
 task:=photo.semantic_uuid_v5(p_child,'semantic.adopt_shown_retouch_region:'||(p_params->>'target_id')::uuid::text);
 params:=photo.semantic_uuid_v5(task,'Params');
 IF p_node->>'id' IS DISTINCT FROM task::text OR p_node->>'params_artifact_id' IS DISTINCT FROM params::text
 OR p_transport->>'task_id' IS DISTINCT FROM task::text OR p_inventory->>'task_id' IS DISTINCT FROM task::text
 OR NOT(p_inventory ?& ARRAY['task_id','entries']) OR (p_inventory-'task_id'-'entries')<>'{}'::jsonb
 OR NOT(p_transport ?& ARRAY['task_id','entry_count','entries_sha','manifest','chunks','direct_roots'])
 OR (p_transport-'task_id'-'entry_count'-'entries_sha'-'manifest'-'chunks'-'direct_roots')<>'{}'::jsonb THEN RETURN false;END IF;
 entries:=p_inventory->'entries';
 IF photo.semantic_shown_retouch_inventory_digest_ready(task,params,p_node->>'params_sha',
 p_node->>'input_digest',p_transport->>'entries_sha',entries) IS DISTINCT FROM true THEN RETURN false;END IF;
 n:=jsonb_array_length(entries);
 SELECT jsonb_agg(e.value->'ref' ORDER BY e.ordinality),
 '['||string_agg('{"contract":"'||(e.value->>'contract')||'","ref":{"artifact_id":"'||(e.value->'ref'->>'artifact_id')||'","sha256":"'||(e.value->'ref'->>'sha256')||'"}}',',' ORDER BY e.ordinality)||']'
 INTO roots,entry_body FROM jsonb_array_elements(entries) WITH ORDINALITY e(value,ordinality);
 IF p_transport->'direct_roots' IS DISTINCT FROM roots OR p_transport->'entry_count' IS DISTINCT FROM to_jsonb(n)
 OR jsonb_typeof(p_transport->'chunks') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_transport->'chunks')<>1 THEN RETURN false;END IF;
 chunk_sha:=encode(sha256(convert_to('{"entries":'||entry_body||',"schema_version":1}','UTF8')),'hex');
 chunk:=jsonb_build_object('artifact_id',photo.semantic_uuid_v5(task,'v2_input_chunk_0'),'sha256',chunk_sha);
 IF p_transport->'chunks' IS DISTINCT FROM jsonb_build_array(chunk) THEN RETURN false;END IF;
 manifest_sha:=encode(sha256(convert_to('{"chunks":[{"artifact_id":"'||(chunk->>'artifact_id')||'","sha256":"'||chunk_sha||'"}],"entry_count":'||n::text||',"entries_sha":"'||(p_transport->>'entries_sha')||'","schema_version":1,"task_id":"'||task::text||'"}','UTF8')),'hex');
 manifest:=jsonb_build_object('artifact_id',photo.semantic_uuid_v5(task,'v2_input_manifest'),'sha256',manifest_sha);
 IF p_transport->'manifest' IS DISTINCT FROM manifest THEN RETURN false;END IF;
 FOR required IN SELECT * FROM (VALUES
 (p_params->'approved_edit','ApprovedSemanticEdit'),(p_params->'presentation','RetouchPreviewPresentation'),
 (p_params->'numerical_evidence','RetouchPreviewNumericalEvidence'),(p_params->'working_image','WorkingImage'),
 (p_params->'pixels','NpyArray')) AS required_roots(ref,contract) LOOP
 IF required.ref IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(entries) item
 WHERE item->'ref'=required.ref AND item->>'contract'=required.contract) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_retouch_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_retouch_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
