-- +goose Up
-- Ordered retained bindings only; physical root bytes/discovery/coverage remain
-- independent. Does not seal or queue native tasks.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_mask_root_inventory_matches(p_content jsonb,p_parser jsonb,p_manual jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE native jsonb;roots jsonb;face jsonb;mask jsonb;root jsonb;expected jsonb;name text;ordinal integer:=0;index_root integer:=0;entry jsonb;
BEGIN
 IF photo.semantic_defect_activation_content_matches(p_content) IS NOT TRUE
 OR jsonb_typeof(p_parser) IS DISTINCT FROM 'object' OR p_parser->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(p_parser->'roots') IS DISTINCT FROM 'array' OR jsonb_array_length(p_parser->'roots')>128 OR octet_length(p_parser::text)>8388608
 OR jsonb_typeof(p_manual) IS DISTINCT FROM 'array' OR jsonb_array_length(p_manual)>64 OR octet_length(p_manual::text)>8388608 THEN RETURN false;END IF;
 native:=(p_content->>'params_text')::jsonb;roots:=p_parser->'roots';
 FOR face IN SELECT value FROM jsonb_array_elements(native->'faces') LOOP
  FOREACH name IN ARRAY ARRAY['skin','manual'] LOOP
   mask:=face->name;
   IF mask IS NULL OR mask='null' THEN CONTINUE;END IF;
   root:=roots->index_root;index_root:=index_root+1;
   expected:=jsonb_build_object('Ordinal',ordinal,'ObjectID',coalesce(face->'object_id','null'),'Landmarks',face->'landmarks','ManualMode',face->'manual_mode','Mask',mask);
   IF jsonb_typeof(root) IS DISTINCT FROM 'object' OR (root-'Root'-'Binding')<>'{}'
   OR root->'Binding' IS DISTINCT FROM expected
   OR jsonb_typeof(root->'Root') IS DISTINCT FROM 'object' OR ((root->'Root')-'artifact_id'-'sha256')<>'{}'
   OR jsonb_typeof(root->'Root'->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(root->'Root'->'sha256') IS DISTINCT FROM 'string'
   OR coalesce(root->'Root'->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR root->'Root'->>'artifact_id'='00000000-0000-0000-0000-000000000000'
   OR coalesce(root->'Root'->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
   IF mask->>'selector'='manual' AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_manual) m WHERE m->'root'=root->'Root' AND m->'target'=mask->'artifact') THEN RETURN false;END IF;
  END LOOP;
  ordinal:=ordinal+1;
 END LOOP;
 IF index_root<>jsonb_array_length(roots)
 OR (SELECT count(DISTINCT value->'Root'->>'artifact_id') FROM jsonb_array_elements(roots))<>(SELECT count(DISTINCT value->'Root') FROM jsonb_array_elements(roots)) THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_manual) LOOP
  IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(roots) r WHERE r->'Root'=entry->'root' AND r->'Binding'->'Mask'->>'selector'='manual' AND r->'Binding'->'Mask'->'artifact'=entry->'target') THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_defect_mask_root_inventory_matches(jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_mask_root_inventory_matches(jsonb,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_mask_root_inventory_matches(jsonb,jsonb,jsonb) TO photo_scheduler;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) RENAME TO lock_semantic_defect_admission_evidence_pre_roots;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence_pre_roots(uuid,uuid,jsonb) FROM photo_scheduler;
CREATE FUNCTION photo.lock_semantic_defect_admission_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_defect_mask_root_inventory_matches(p_evidence->'content',p_evidence->'parser',p_evidence->'manual') IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_admission_evidence_pre_roots(p_tenant,p_run,p_evidence);
END $$;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
