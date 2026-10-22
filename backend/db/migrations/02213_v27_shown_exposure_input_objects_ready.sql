-- +goose Up
-- Physical metadata gate only. Exact inventory, producer provenance, publication
-- scope and active tenant/Apply authority must be verified by atomic admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_exposure_input_objects_ready(p_tenant uuid,p_entries jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE entry jsonb;ref jsonb;contract_name text;aid uuid;prior uuid;a photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(p_entries) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_entries) NOT BETWEEN 1 AND 512 THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_entries) LOOP
  ref:=entry->'ref';contract_name:=entry->>'contract';
  IF jsonb_typeof(entry) IS DISTINCT FROM 'object' OR NOT(entry ?& ARRAY['ref','contract'])
  OR (entry-'ref'-'contract')<>'{}'::jsonb OR jsonb_typeof(entry->'contract') IS DISTINCT FROM 'string'
  OR contract_name NOT IN('Params','SemanticPlan','WorkingImage','EditIntent','TransferCandidate','MaskRef',
  'ResolvedTransform','NpyArray','ApprovedSemanticEdit','ExposurePreviewPresentation','ExposurePreviewNumericalEvidence','EncodedImage')
  OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT(ref ?& ARRAY['artifact_id','sha256'])
  OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF aid='00000000-0000-0000-0000-000000000000'::uuid OR (prior IS NOT NULL AND aid<=prior) THEN RETURN false;END IF;
  prior:=aid;
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=aid;
  IF NOT FOUND OR a.sha256 IS DISTINCT FROM ref->>'sha256' OR a.state IS DISTINCT FROM 'ready'
  OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
  OR coalesce(a.bucket,'')='' OR coalesce(a.object_key,'')=''
  OR a.bytes IS NULL OR a.bytes NOT BETWEEN 1 AND 9007199254740991
  OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
  IF contract_name='NpyArray' THEN
   IF coalesce(a.media_type,'') NOT IN('application/x-npy','application/octet-stream') OR a.bytes>2400016384 THEN RETURN false;END IF;
  ELSIF contract_name='EncodedImage' THEN
   IF a.media_type IS DISTINCT FROM 'image/png' OR a.bytes>33882112 THEN RETURN false;END IF;
  ELSE
   IF a.media_type IS DISTINCT FROM 'application/json' OR a.bytes>1048576 THEN RETURN false;END IF;
  END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_exposure_input_objects_ready(uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_exposure_input_objects_ready(uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
