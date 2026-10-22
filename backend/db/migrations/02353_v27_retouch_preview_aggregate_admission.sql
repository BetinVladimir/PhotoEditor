-- +goose Up
-- Private aggregate pre-materialization gate; creates no task or approval.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_admission_inputs_ready(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,
 p_body text,p_sha text,p_candidate_body text,p_intent_body text,p_source_body text,
 p_reference_body text,p_mask_body text,p_transform_body text,p_protection_body text,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;protection jsonb;project uuid;raw text;profile jsonb;context_doc jsonb;face jsonb;control text;
 quota photo.quota_reservations%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_node) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_transport) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 PERFORM photo.lock_semantic_retouch_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_sha);
 FOREACH raw IN ARRAY ARRAY[p_candidate_body,p_intent_body,p_source_body,p_reference_body,
 p_mask_body,p_transform_body,p_protection_body] LOOP
 IF raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 END LOOP;
 e:=p_body::jsonb;protection:=p_protection_body::jsonb;
 SELECT config_snapshot->'semantic_retouch_preview'->'adapter_context' INTO context_doc
 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_preview;
 IF context_doc->'source' IS DISTINCT FROM e->'source'
 OR context_doc->'candidate' IS DISTINCT FROM e->'candidate'
 OR context_doc->'protection' IS DISTINCT FROM e->'protection'
 OR context_doc->>'model_lock_sha' IS DISTINCT FROM e->>'model_lock_sha'
 OR context_doc->>'runtime_lock_sha' IS DISTINCT FROM e->>'runtime_lock_sha'
 OR context_doc->>'device_profile_sha' IS DISTINCT FROM e->>'device_profile_sha'
 OR context_doc->'beauty'->'enabled' IS DISTINCT FROM 'true'::jsonb
 OR context_doc->'beauty'->>'algorithm' IS DISTINCT FROM 'beautyfilter_v1'
 OR context_doc->'beauty'->>'skin_mask' IS DISTINCT FROM 'face68_conservative_v1'
 OR context_doc->'beauty'->'protect_features' IS DISTINCT FROM 'true'::jsonb THEN RETURN false;END IF;
 FOREACH control IN ARRAY ARRAY['smoothing','tone_evenness','texture_keep'] LOOP
 IF jsonb_typeof(context_doc->'beauty'->control) IS DISTINCT FROM 'number'
 OR (context_doc->'beauty'->>control)::numeric NOT BETWEEN 0 AND 1 THEN RETURN false;END IF;
 END LOOP;
 IF photo.semantic_retouch_preview_documents_ready(p_tenant,p_preview,e,p_node,p_transport,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_retouch_preview_sidecars_ready(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_candidate_body,p_mask_body,p_transform_body,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_retouch_preview_images_ready(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_intent_body,p_source_body,p_reference_body,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_retouch_preview_inventory_exact(e,p_node,p_inventory,p_candidate_body::jsonb,
 p_intent_body::jsonb,p_source_body::jsonb,p_reference_body::jsonb,p_mask_body::jsonb,protection,context_doc) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 IF protection->'protected' IS DISTINCT FROM 'true'::jsonb
 OR encode(public.digest(convert_to(p_protection_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM e->'protection'->>'sha256'
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant
 AND id=(e->'protection'->>'artifact_id')::uuid AND sha256=e->'protection'->>'sha256'
 AND bytes=octet_length(p_protection_body)) THEN RETURN false;END IF;
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF photo.semantic_retouch_preview_protection_ready(p_tenant,project,e,
 (protection->'artifact'->>'artifact_id')::uuid,protection->'artifact'->>'sha256') IS DISTINCT FROM true THEN RETURN false;END IF;
 FOR face IN SELECT value FROM jsonb_array_elements(context_doc->'faces') LOOP
 IF photo.semantic_retouch_preview_landmark_origin(p_tenant,project,p_actor,p_session,e->'source',
 (face->'landmarks'->>'artifact_id')::uuid,face->'landmarks'->>'sha256') IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 profile:=p_node->'execution_profile';
 IF photo.v27_downstream_profile_ready(p_tenant,project,'semantic.preview_retouch_region','1','cpu',profile) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_qualification_policies policy
 WHERE policy.id=(profile->'qualification'->>'policy_id')::uuid
 AND policy.operation='semantic.preview_retouch_region' AND policy.operation_version='1' AND policy.backend='cpu'
 AND policy.required_checks @> ARRAY['retouch.real_photo_review','retouch.outside_mask_exact','retouch.protected_pixels_exact',
 'retouch.alpha_preserved','retouch.strength_zero_exact','retouch.strength_once','retouch.texture_keep',
 'retouch.full_face_roi','retouch.landmark_source','retouch.model_runtime_pins','retouch.cancellation',
 'retouch.resource_admission','retouch.hdr_refusal']::text[]) THEN RETURN false;END IF;
 -- Preserve tenant -> parent -> target -> preview -> usage -> quota lock order.
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO quota FROM photo.quota_reservations WHERE tenant_id=p_tenant
 AND holder_type='run' AND holder_id=p_preview FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN coalesce(quota.state='reserved' AND quota.principal_type='user' AND quota.principal_id=p_actor
 AND quota.share_id IS NULL AND quota.files=0 AND quota.bytes=(profile->'resource_estimate'->>'output_bytes')::bigint
 AND quota.expires_at>clock_timestamp(),false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_admission_inputs_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_admission_inputs_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
