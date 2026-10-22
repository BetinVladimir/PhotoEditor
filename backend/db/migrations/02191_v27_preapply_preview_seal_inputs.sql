-- +goose Up
-- Private transaction component: register and seal exact inputs, still blocked.
-- Final caller must activate or roll back this entire transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_semantic_preapply_preview_inputs(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,
 p_body text,p_sha text,p_candidate_body text,p_intent_body text,p_source_body text,
 p_reference_body text,p_mask_body text,p_transform_body text,p_protection_body text,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE owner uuid;registered integer;sealed_profile jsonb;sealed_inputs jsonb;
BEGIN
 registered:=photo.register_semantic_preapply_preview_inputs(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_candidate_body,p_intent_body,p_source_body,p_reference_body,p_mask_body,p_transform_body,
 p_protection_body,p_node,p_transport,p_inventory);
 IF registered IS DISTINCT FROM jsonb_array_length(p_inventory->'entries')+2 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INPUT_REGISTRATION_INCOMPLETE' USING ERRCODE='23514';END IF;
 owner:=(p_node->>'id')::uuid;
 sealed_inputs:=jsonb_build_array(p_transport->'manifest')||(p_transport->'direct_roots');
 sealed_profile:=(p_node->'execution_profile')||jsonb_build_object(
 'renderer_version','v2.2','contracts_sha','13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5',
 'v2_input_manifest',p_transport->'manifest','v2_entry_count',(p_transport->>'entry_count')::integer,
 'v2_entries_sha',p_transport->>'entries_sha');
 UPDATE photo.tasks SET inputs=sealed_inputs,execution_profile=sealed_profile
 WHERE tenant_id=p_tenant AND run_id=p_preview AND id=owner
 AND state='blocked' AND attempt=0 AND NOT cancel_requested
 AND inputs='[]'::jsonb AND execution_profile=p_node->'execution_profile';
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_PREVIEW_SEAL_STALE' USING ERRCODE='40001';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_preview AND id=owner
 AND state='blocked' AND attempt=0 AND NOT cancel_requested
 AND inputs=sealed_inputs AND execution_profile=sealed_profile
 AND params_artifact_id=(p_node->>'params_artifact_id')::uuid AND input_digest=p_node->>'input_digest') THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_SEAL_INCOMPLETE' USING ERRCODE='23514';END IF;
 RETURN owner;
END $$;
ALTER FUNCTION photo.seal_semantic_preapply_preview_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_preapply_preview_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
