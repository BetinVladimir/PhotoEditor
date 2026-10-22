-- +goose Up
-- Candidate-child helpers below only lock a proposed review target; despite
-- historical names they neither approve an edit nor require an Apply child.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_sidecars_ready(p_tenant uuid,p_intent uuid,
 p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,p_body text,p_sha text,
 p_candidate_body text,p_mask_body text,p_transform_body text,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;mask record;transform record;mask_binary record;required record;
BEGIN
 PERFORM photo.lock_semantic_preapply_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_sha);
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
 SELECT * INTO mask_binary FROM photo.semantic_approved_mask_binary(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,
 e->'candidate'->>'sha256',p_candidate_body,p_mask_body);
 IF mask.artifact_id IS NULL OR transform.artifact_id IS NULL OR mask_binary.artifact_id IS NULL
 OR mask.artifact_id=transform.artifact_id OR mask.artifact_id=mask_binary.artifact_id
 OR transform.artifact_id=mask_binary.artifact_id OR mask.bytes<>octet_length(p_mask_body)
 OR transform.bytes<>octet_length(p_transform_body)
 OR encode(public.digest(convert_to(p_transform_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM transform.sha256 THEN RETURN false;END IF;
 FOR required IN SELECT * FROM (VALUES
 (mask.artifact_id,mask.sha256,'MaskRef'),(transform.artifact_id,transform.sha256,'ResolvedTransform'),
 (mask_binary.artifact_id,mask_binary.sha256,'NpyArray')) AS sidecars(id,sha,contract) LOOP
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_inventory->'entries') item
 WHERE item->'ref'=jsonb_build_object('artifact_id',required.id,'sha256',required.sha)
 AND item->>'contract'=required.contract) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_preapply_preview_sidecars_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_sidecars_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
