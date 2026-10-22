-- +goose Up
-- Private same-transaction registration. No sealing, task readiness or dispatch.
-- Partial or altered previous registration is a conflict, never silently repaired.
-- +goose StatementBegin
CREATE FUNCTION photo.register_semantic_preapply_preview_inputs(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,
 p_body text,p_sha text,p_candidate_body text,p_intent_body text,p_source_body text,
 p_reference_body text,p_mask_body text,p_transform_body text,p_protection_body text,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE owner uuid;project uuid;expected jsonb;actual jsonb;entry jsonb;total integer;
BEGIN
 owner:=photo.create_semantic_preapply_preview_draft_task(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_candidate_body,p_intent_body,p_source_body,p_reference_body,p_mask_body,p_transform_body,
 p_protection_body,p_node,p_transport,p_inventory);
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF photo.semantic_preapply_preview_input_scope(p_tenant,project,owner) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INPUT_SCOPE_INVALID' USING ERRCODE='23514';END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',ref,'contract',contract,'ordinal',ordinal) ORDER BY ordinal)
 INTO expected FROM (
 SELECT e.value->'ref' AS ref,e.value->>'contract' AS contract,(e.ordinality-1)::integer AS ordinal
 FROM jsonb_array_elements(p_inventory->'entries') WITH ORDINALITY e(value,ordinality)
 UNION ALL SELECT p_transport->'chunks'->0,'InputClosureChunk',1000000
 UNION ALL SELECT p_transport->'manifest','InputClosureManifest',1000001
 ) entries;
 total:=jsonb_array_length(expected);
 SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),
 'contract',i.contract,'ordinal',i.ordinal) ORDER BY i.ordinal),'[]'::jsonb)
 INTO actual FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=owner;
 IF actual<>'[]'::jsonb THEN
 IF actual IS DISTINCT FROM expected OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.task_id=owner AND i.project_id IS DISTINCT FROM project) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INPUT_REPLAY_CONFLICT' USING ERRCODE='40001';END IF;
 RETURN total;
 END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(expected) LOOP
 INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
 VALUES(p_tenant,project,owner,(entry->'ref'->>'artifact_id')::uuid,entry->'ref'->>'sha256',
 entry->>'contract',(entry->>'ordinal')::integer);
 END LOOP;
 RETURN total;
END $$;
ALTER FUNCTION photo.register_semantic_preapply_preview_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_semantic_preapply_preview_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
