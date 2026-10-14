-- +goose Up
-- Qualified donor inventory digest for staging; provenance is checked separately.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_donor_inventory_digest_ready(p_task uuid,p_params uuid,p_params_sha text,p_operation text,p_input_digest text,p_entries jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; ref jsonb; aid uuid; prior uuid; contract_name text; body text:='['; first_item boolean:=true; params_count integer:=0;
BEGIN
 IF p_task IS NULL OR p_task='00000000-0000-0000-0000-000000000000'::uuid OR p_params IS NULL
 OR p_params='00000000-0000-0000-0000-000000000000'::uuid
 OR coalesce(p_params_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_input_digest,'') !~ '^[0-9a-f]{64}$'
 OR p_operation IS NULL OR p_operation<>'portrait.open_eyes' OR jsonb_typeof(p_entries) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_entries) NOT BETWEEN 1 AND 127 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_entries) LOOP
  ref:=item->'ref';contract_name:=item->>'contract';
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR NOT(item ?& ARRAY['ref','contract'])
  OR (item-'ref'-'contract')<>'{}'::jsonb OR jsonb_typeof(item->'contract') IS DISTINCT FROM 'string'
  OR contract_name NOT IN('Params','WorkingImage','MaskRef','RasterizedMask','ManualMask','MaskPixels','NpyArray','Artifact','Report','Calibration','DonorConfirmation','FaceObservations')
  OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT(ref ?& ARRAY['artifact_id','sha256'])
  OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF aid='00000000-0000-0000-0000-000000000000'::uuid OR (prior IS NOT NULL AND aid<=prior) THEN RETURN false;END IF;
  IF aid=p_params THEN
   params_count:=params_count+1;
   IF contract_name<>'Params' OR ref->>'sha256'<>p_params_sha THEN RETURN false;END IF;
  END IF;
  IF NOT first_item THEN body:=body||',';END IF;
  body:=body||'{"contract":"'||contract_name||'","ref":{"artifact_id":"'||aid::text||'","sha256":"'||(ref->>'sha256')||'"}}';
  first_item:=false;prior:=aid;
 END LOOP;
 IF EXISTS(SELECT 1 FROM (VALUES('Params'::text,1),('Report'::text,1),('Calibration'::text,1),
 ('DonorConfirmation'::text,1),('FaceObservations'::text,2),('WorkingImage'::text,2)) required(contract,amount)
 WHERE (SELECT count(*) FROM jsonb_array_elements(p_entries) e WHERE e->>'contract'=required.contract)<>required.amount) THEN RETURN false;END IF;
 body:=body||']';
 RETURN params_count=1 AND encode(sha256(convert_to('{"entries":'||body||',"operation":"'||p_operation||'","task":"'||p_task::text||'","version":"1"}','UTF8')),'hex')=p_input_digest;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_donor_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
