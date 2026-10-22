-- +goose Up
-- Pure exact dependency projection. Caller must hash-bind every JSON document
-- and prove live authenticated producer/physical provenance before admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_inventory_exact(p_params jsonb,p_node jsonb,p_inventory jsonb,
 p_candidate jsonb,p_intent jsonb,p_source jsonb,p_reference jsonb,p_mask jsonb,p_protection jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;count_entries integer;unique_ids integer;
BEGIN
 IF jsonb_typeof(p_inventory->'entries') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_candidate) IS DISTINCT FROM 'object' OR jsonb_typeof(p_intent) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_source) IS DISTINCT FROM 'object' OR jsonb_typeof(p_reference) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_mask) IS DISTINCT FROM 'object' OR jsonb_typeof(p_protection) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_source->'transform_chain'->'steps') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_reference->'transform_chain'->'steps') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 WITH roots(ref,contract) AS (
 SELECT * FROM (VALUES
 (jsonb_build_object('artifact_id',p_node->>'params_artifact_id','sha256',p_node->>'params_sha'),'Params'),
 (p_params->'plan','SemanticPlan'),(p_params->'intent','EditIntent'),(p_params->'candidate','TransferCandidate'),
 (p_params->'source'->'source','WorkingImage'),(p_params->'protection','MaskRef'),
 (p_candidate->'mask','MaskRef'),(p_candidate->'transform','ResolvedTransform'),
 (p_intent->'reference'->'source','WorkingImage'),(p_source->'pixels','NpyArray'),
 (p_reference->'pixels','NpyArray'),(p_mask->'artifact','NpyArray'),(p_protection->'artifact','NpyArray')) AS fixed_roots(ref,contract)
 UNION ALL SELECT p_source->'alpha','NpyArray' WHERE p_source->'alpha' IS NOT NULL AND p_source->'alpha'<>'null'::jsonb
 UNION ALL SELECT p_reference->'alpha','NpyArray' WHERE p_reference->'alpha' IS NOT NULL AND p_reference->'alpha'<>'null'::jsonb
 UNION ALL SELECT step->'inverse_field','NpyArray' FROM jsonb_array_elements(p_source->'transform_chain'->'steps') step WHERE step->>'kind'='dense_warp'
 UNION ALL SELECT step->'inverse_field','NpyArray' FROM jsonb_array_elements(p_reference->'transform_chain'->'steps') step WHERE step->>'kind'='dense_warp'
 ),deduplicated AS (SELECT DISTINCT ref,contract FROM roots)
 SELECT jsonb_agg(jsonb_build_object('ref',ref,'contract',contract) ORDER BY (ref->>'artifact_id')::uuid),
 count(*)::integer,count(DISTINCT ref->>'artifact_id')::integer INTO expected,count_entries,unique_ids FROM deduplicated;
 IF count_entries NOT BETWEEN 1 AND 512 OR count_entries<>unique_ids
 OR expected IS DISTINCT FROM p_inventory->'entries' THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_preapply_preview_inventory_exact(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_inventory_exact(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
