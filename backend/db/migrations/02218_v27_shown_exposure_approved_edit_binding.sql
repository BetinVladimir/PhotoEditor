-- +goose Up
-- Exact semantic edit content binding only. The caller must prove artifact
-- ownership and inclusion in the authenticated committed decision batch.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_exposure_approved_edit_bound(p_ref jsonb,p_body text,
 p_preview_params jsonb,p_candidate jsonb,p_intent jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_ref) IS DISTINCT FROM 'object' OR NOT(p_ref ?& ARRAY['artifact_id','sha256'])
 OR (p_ref-'artifact_id'-'sha256')<>'{}'::jsonb
 OR jsonb_typeof(p_ref->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(p_ref->'sha256') IS DISTINCT FROM 'string'
 OR coalesce(p_ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR p_ref->>'artifact_id'='00000000-0000-0000-0000-000000000000'
 OR coalesce(p_ref->>'sha256','') !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_ref->>'sha256'
 OR jsonb_typeof(p_preview_params) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_candidate) IS DISTINCT FROM 'object' OR jsonb_typeof(p_intent) IS DISTINCT FROM 'object'
 THEN RETURN false;END IF;
 e:=p_body::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object'
 OR NOT(e ?& ARRAY['schema_version','target_id','source','intent','candidate','mask','transform','kind','strength','ev','prompt','preserve_persons'])
 OR (e-ARRAY['schema_version','target_id','source','intent','candidate','mask','transform','kind','strength','ev','prompt','preserve_persons'])<>'{}'::jsonb
 OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR e->'target_id' IS DISTINCT FROM p_preview_params->'target_id'
 OR e->'source' IS DISTINCT FROM p_preview_params->'source'
 OR e->'intent' IS DISTINCT FROM p_preview_params->'intent'
 OR e->'candidate' IS DISTINCT FROM p_preview_params->'candidate'
 OR e->'mask' IS DISTINCT FROM p_candidate->'mask'
 OR e->'transform' IS DISTINCT FROM p_candidate->'transform'
 OR jsonb_typeof(e->'mask') IS DISTINCT FROM 'object' OR jsonb_typeof(e->'transform') IS DISTINCT FROM 'object'
 OR e->>'kind' IS DISTINCT FROM 'local_exposure' OR p_intent->>'kind' IS DISTINCT FROM 'local_exposure'
 OR e->'preserve_persons' IS DISTINCT FROM 'true'::jsonb OR p_intent->'preserve_persons' IS DISTINCT FROM 'true'::jsonb
 OR jsonb_typeof(e->'ev') IS DISTINCT FROM 'number' OR jsonb_typeof(e->'strength') IS DISTINCT FROM 'number'
 OR e->'ev' IS DISTINCT FROM p_intent->'ev' OR e->'strength' IS DISTINCT FROM p_intent->'strength'
 OR e->'prompt' IS DISTINCT FROM coalesce(p_intent->'prompt','null'::jsonb) THEN RETURN false;END IF;
 RETURN (e->>'ev')::numeric BETWEEN -1 AND 1 AND (e->>'strength')::numeric BETWEEN 0 AND 1;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_exposure_approved_edit_bound(jsonb,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_exposure_approved_edit_bound(jsonb,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
