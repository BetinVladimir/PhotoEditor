-- +goose Up
-- Candidate-child helpers below only lock a proposed review target; despite
-- historical names they neither approve an edit nor require an Apply child.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_sidecars_ready(p_tenant uuid,p_intent uuid,
 p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,p_body text,p_sha text,
 p_candidate_body text,p_mask_body text,p_transform_body text,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;mask record;transform record;image_binary record;required record;
BEGIN
 PERFORM photo.lock_semantic_retouch_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_sha);
 e:=p_body::jsonb;
 IF p_candidate_body IS NULL OR octet_length(p_candidate_body) NOT BETWEEN 2 AND 1048576
 OR p_mask_body IS NULL OR octet_length(p_mask_body) NOT BETWEEN 2 AND 1048576
 OR p_transform_body IS NULL OR octet_length(p_transform_body) NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_inventory->'entries') IS DISTINCT FROM 'array'
 OR (p_candidate_body::jsonb)->'target' IS DISTINCT FROM e->'source' THEN RETURN false;END IF;
 SELECT * INTO mask FROM photo.semantic_approved_candidate_child(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,
 e->'candidate'->>'sha256',p_candidate_body,'mask');
 SELECT * INTO transform FROM photo.semantic_approved_candidate_child(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,
 e->'candidate'->>'sha256',p_candidate_body,'transform');
 SELECT * INTO image_binary FROM photo.semantic_approved_mask_binary(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,
 e->'candidate'->>'sha256',p_candidate_body,p_mask_body);
 IF mask.artifact_id IS NULL OR transform.artifact_id IS NULL OR image_binary.artifact_id IS NULL
 OR mask.artifact_id=transform.artifact_id OR mask.artifact_id=image_binary.artifact_id
 OR transform.artifact_id=image_binary.artifact_id OR mask.bytes<>octet_length(p_mask_body)
 OR transform.bytes<>octet_length(p_transform_body)
 OR encode(public.digest(convert_to(p_transform_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM transform.sha256 THEN RETURN false;END IF;
 FOR required IN SELECT * FROM (VALUES
 (mask.artifact_id,mask.sha256,'MaskRef'),(transform.artifact_id,transform.sha256,'ResolvedTransform'),
 (image_binary.artifact_id,image_binary.sha256,'NpyArray')) AS sidecars(id,sha,contract) LOOP
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_inventory->'entries') item
 WHERE item->'ref'=jsonb_build_object('artifact_id',required.id,'sha256',required.sha)
 AND item->>'contract'=required.contract) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_sidecars_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_sidecars_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Private proposed-target image/dependency provenance, without Apply approval.
-- Physical arrays, ICC and geometry require independent worker verification.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_images_ready(p_tenant uuid,p_intent uuid,
 p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,p_body text,p_sha text,
 p_intent_body text,p_source_body text,p_reference_body text,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;intent jsonb;image jsonb;pin jsonb;raw text;root record;image_binary record;child record;is_reference boolean;
BEGIN
 PERFORM photo.lock_semantic_retouch_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_sha);
 e:=p_body::jsonb;
 IF p_intent_body IS NULL OR octet_length(p_intent_body) NOT BETWEEN 2 AND 1048576
 OR p_source_body IS NULL OR octet_length(p_source_body) NOT BETWEEN 2 AND 1048576
 OR p_reference_body IS NULL OR octet_length(p_reference_body) NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_inventory->'entries') IS DISTINCT FROM 'array'
 OR encode(public.digest(convert_to(p_intent_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM e->'intent'->>'sha256'
 THEN RETURN false;END IF;
 intent:=p_intent_body::jsonb;
 IF intent->>'kind' IS DISTINCT FROM 'retouch_region' OR intent->'ev' IS DISTINCT FROM 'null'::jsonb OR intent->'preserve_persons' IS DISTINCT FROM 'true'::jsonb
 OR intent->>'target_snapshot_id' IS DISTINCT FROM e->>'snapshot_id' THEN RETURN false;END IF;
 FOREACH is_reference IN ARRAY ARRAY[false,true] LOOP
 pin:=CASE WHEN is_reference THEN intent->'reference'->'source' ELSE e->'source'->'source' END;
 raw:=CASE WHEN is_reference THEN p_reference_body ELSE p_source_body END;
 SELECT * INTO root FROM photo.semantic_approved_image_object(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,e->'candidate'->>'sha256',
 is_reference,(pin->>'artifact_id')::uuid,pin->>'sha256');
 IF root.artifact_id IS NULL OR root.bytes<>octet_length(raw)
 OR encode(public.digest(convert_to(raw,'UTF8'),'sha256'),'hex') IS DISTINCT FROM root.sha256
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_inventory->'entries') item
 WHERE item->'ref'=pin AND item->>'contract'='WorkingImage') THEN RETURN false;END IF;
 image:=raw::jsonb;
 IF jsonb_typeof(image->'pixels') IS DISTINCT FROM 'object'
 OR jsonb_typeof(image->'transform_chain'->'steps') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(image->'transform_chain'->'steps')>32 THEN RETURN false;END IF;
 FOR child IN
 SELECT 'pixels'::text AS kind,-1 AS step
 UNION ALL SELECT 'alpha',-1 WHERE image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb
 UNION ALL SELECT 'inverse_field',(s.ordinality-1)::integer
 FROM jsonb_array_elements(image->'transform_chain'->'steps') WITH ORDINALITY s(value,ordinality)
 WHERE s.value->>'kind'='dense_warp'
 LOOP
 SELECT * INTO image_binary FROM photo.semantic_approved_image_binary(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,e->'candidate'->>'sha256',
 is_reference,(pin->>'artifact_id')::uuid,pin->>'sha256',raw,child.kind,child.step);
 IF image_binary.artifact_id IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_inventory->'entries') item
 WHERE item->'ref'=jsonb_build_object('artifact_id',image_binary.artifact_id,'sha256',image_binary.sha256)
 AND item->>'contract'='NpyArray') THEN RETURN false;END IF;
 END LOOP;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_images_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_images_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Pure exact dependency projection. Caller must hash-bind every JSON document
-- and prove live authenticated producer/physical provenance before admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_inventory_exact(p_params jsonb,p_node jsonb,p_inventory jsonb,
 p_candidate jsonb,p_intent jsonb,p_source jsonb,p_reference jsonb,p_mask jsonb,p_protection jsonb,p_context jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;count_entries integer;unique_ids integer;
BEGIN
 IF jsonb_typeof(p_inventory->'entries') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_candidate) IS DISTINCT FROM 'object' OR jsonb_typeof(p_intent) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_source) IS DISTINCT FROM 'object' OR jsonb_typeof(p_reference) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_mask) IS DISTINCT FROM 'object' OR jsonb_typeof(p_protection) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_context) IS DISTINCT FROM 'object'
 OR p_context->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_context->>'algorithm' IS DISTINCT FROM 'beautyfilter_v1'
 OR p_context->'source' IS DISTINCT FROM p_params->'source'
 OR p_context->'candidate' IS DISTINCT FROM p_params->'candidate'
 OR p_context->'protection' IS DISTINCT FROM p_params->'protection'
 OR p_context->'transferred_mask' IS DISTINCT FROM p_candidate->'mask'
 OR jsonb_typeof(p_context->'faces') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_source->'transform_chain'->'steps') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_reference->'transform_chain'->'steps') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_context->'faces') NOT BETWEEN 1 AND 64
 OR p_mask->>'selector' IS DISTINCT FROM 'manual' OR p_mask->'protected' IS DISTINCT FROM 'false'::jsonb
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(p_context->'faces') face
 WHERE face->'manual_mode' IS DISTINCT FROM 'true'::jsonb OR face->'manual' IS DISTINCT FROM p_mask
 OR jsonb_typeof(face->'landmarks') IS DISTINCT FROM 'object'
 OR (face->'skin' IS NOT NULL AND face->'skin'<>'null'::jsonb AND (
 jsonb_typeof(face->'skin') IS DISTINCT FROM 'object' OR face->'skin'->>'selector' IS DISTINCT FROM 'skin'
 OR face->'skin'->'protected' IS DISTINCT FROM 'false'::jsonb))) THEN RETURN false;END IF;
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
 UNION ALL SELECT p_node->'execution_profile'->'retouch_context','RetouchPreviewAdapterContext'
 UNION ALL SELECT face->'landmarks',CASE WHEN face->>'object_id' IS NULL THEN 'Face68' ELSE 'FaceLandmarks' END
 FROM jsonb_array_elements(p_context->'faces') face
 UNION ALL SELECT face->'manual'->'artifact','NpyArray' FROM jsonb_array_elements(p_context->'faces') face
 UNION ALL SELECT face->'skin'->'artifact','NpyArray' FROM jsonb_array_elements(p_context->'faces') face
 WHERE face->'skin' IS NOT NULL AND face->'skin'<>'null'::jsonb
 ),deduplicated AS (SELECT DISTINCT ref,contract FROM roots)
 SELECT jsonb_agg(jsonb_build_object('ref',ref,'contract',contract) ORDER BY (ref->>'artifact_id')::uuid),
 count(*)::integer,count(DISTINCT ref->>'artifact_id')::integer INTO expected,count_entries,unique_ids FROM deduplicated;
 IF count_entries NOT BETWEEN 1 AND 512 OR count_entries<>unique_ids
 OR expected IS DISTINCT FROM p_inventory->'entries' THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_inventory_exact(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_inventory_exact(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
