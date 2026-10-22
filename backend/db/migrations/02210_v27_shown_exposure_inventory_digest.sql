-- +goose Up
-- Independent JCS reconstruction for the shown-result adoption inventory.
-- Allowed contract names, UUIDs and SHA strings are ASCII, so this exact wire
-- grammar requires no general JSON canonicalizer. No authority/admission here.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_exposure_inventory_digest_ready(p_task uuid,p_params uuid,p_params_sha text,p_input_digest text,p_entries_sha text,p_entries jsonb) RETURNS boolean
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
  OR contract_name NOT IN('Params','SemanticPlan','WorkingImage','EditIntent','TransferCandidate','MaskRef','ResolvedTransform','NpyArray','ApprovedSemanticEdit','ExposurePreviewPresentation','ExposurePreviewNumericalEvidence','EncodedImage')
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
 AND encode(sha256(convert_to('{"entries":'||body||',"operation":"semantic.adopt_shown_exposure","task":"'||p_task::text||'","version":"1"}','UTF8')),'hex')=p_input_digest;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_exposure_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_exposure_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
