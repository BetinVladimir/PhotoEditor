-- +goose Up
-- Private aggregate admission gate. Caller must materialize and seal in the
-- same transaction while these authority/quota locks remain held.
-- This function grants no approval and creates no task.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_admission_inputs_ready(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,
 p_body text,p_sha text,p_candidate_body text,p_intent_body text,p_source_body text,
 p_reference_body text,p_mask_body text,p_transform_body text,p_protection_body text,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;protection jsonb;project uuid;raw text;profile jsonb;
 quota photo.quota_reservations%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_node) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_transport) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 PERFORM photo.lock_semantic_preapply_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_sha);
 FOREACH raw IN ARRAY ARRAY[p_candidate_body,p_intent_body,p_source_body,p_reference_body,
 p_mask_body,p_transform_body,p_protection_body] LOOP
 IF raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 END LOOP;
 e:=p_body::jsonb;protection:=p_protection_body::jsonb;
 IF photo.semantic_preapply_preview_documents_ready(p_tenant,p_preview,e,p_node,p_transport,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_preapply_preview_sidecars_ready(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_candidate_body,p_mask_body,p_transform_body,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_preapply_preview_images_ready(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_intent_body,p_source_body,p_reference_body,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_preapply_preview_inventory_exact(e,p_node,p_inventory,p_candidate_body::jsonb,
 p_intent_body::jsonb,p_source_body::jsonb,p_reference_body::jsonb,p_mask_body::jsonb,protection) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 IF protection->'protected' IS DISTINCT FROM 'true'::jsonb
 OR encode(public.digest(convert_to(p_protection_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM e->'protection'->>'sha256'
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant
 AND id=(e->'protection'->>'artifact_id')::uuid AND sha256=e->'protection'->>'sha256'
 AND bytes=octet_length(p_protection_body)) THEN RETURN false;END IF;
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF photo.semantic_preapply_preview_protection_ready(p_tenant,project,e,
 (protection->'artifact'->>'artifact_id')::uuid,protection->'artifact'->>'sha256') IS DISTINCT FROM true THEN RETURN false;END IF;
 profile:=p_node->'execution_profile';
 IF photo.v27_downstream_profile_ready(p_tenant,project,'semantic.local_exposure','1','cpu',profile) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 -- Preserve tenant -> parent -> target -> preview -> usage -> quota lock order.
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO quota FROM photo.quota_reservations WHERE tenant_id=p_tenant
 AND holder_type='run' AND holder_id=p_preview FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN coalesce(quota.state='reserved' AND quota.principal_type='user' AND quota.principal_id=p_actor
 AND quota.files=0 AND quota.bytes=(profile->'resource_estimate'->>'output_bytes')::bigint
 AND quota.expires_at>clock_timestamp(),false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_preapply_preview_admission_inputs_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_admission_inputs_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
