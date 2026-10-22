-- +goose Up
-- Private transaction component. Exact inventory registration, seal and
-- activation must follow in the same transaction or the caller must roll back.
-- Never changes the photographer's proposed target or creates an Apply child.
-- +goose StatementBegin
CREATE FUNCTION photo.create_semantic_preapply_preview_draft_task(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,
 p_body text,p_sha text,p_candidate_body text,p_intent_body text,p_source_body text,
 p_reference_body text,p_mask_body text,p_transform_body text,p_protection_body text,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task_id uuid;existing photo.tasks%ROWTYPE;
BEGIN
 IF photo.semantic_preapply_preview_admission_inputs_ready(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_candidate_body,p_intent_body,p_source_body,p_reference_body,p_mask_body,p_transform_body,
 p_protection_body,p_node,p_transport,p_inventory) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INPUTS_UNAVAILABLE' USING ERRCODE='23514';END IF;
 task_id:=(p_node->>'id')::uuid;
 SELECT * INTO existing FROM photo.tasks WHERE id=task_id FOR UPDATE;
 IF FOUND THEN
 IF existing.tenant_id IS DISTINCT FROM p_tenant OR existing.run_id IS DISTINCT FROM p_preview
 OR existing.node_key IS DISTINCT FROM p_node->>'node_key'
 OR existing.operation IS DISTINCT FROM 'semantic.preview_local_exposure'
 OR existing.operation_version IS DISTINCT FROM '1' OR existing.resource_class IS DISTINCT FROM 'cpu'
 OR existing.state IS DISTINCT FROM 'blocked' OR existing.attempt IS DISTINCT FROM 0
 OR existing.cancel_requested IS DISTINCT FROM false OR existing.inputs IS DISTINCT FROM '[]'::jsonb
 OR existing.params_artifact_id IS DISTINCT FROM (p_node->>'params_artifact_id')::uuid
 OR existing.input_digest IS DISTINCT FROM p_node->>'input_digest'
 OR existing.execution_profile IS DISTINCT FROM p_node->'execution_profile' THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_TASK_CONFLICT' USING ERRCODE='40001';END IF;
 ELSE
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,
 state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(task_id,p_tenant,p_preview,p_node->>'node_key','semantic.preview_local_exposure','1','cpu',
 'blocked','[]'::jsonb,(p_node->>'params_artifact_id')::uuid,p_node->>'input_digest',p_node->'execution_profile');
 END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_preview AND id<>task_id)
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant
 AND (run_id=p_preview OR prerequisite_id=task_id OR dependent_id=task_id)) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_GRAPH_CONFLICT' USING ERRCODE='23514';END IF;
 RETURN task_id;
END $$;
ALTER FUNCTION photo.create_semantic_preapply_preview_draft_task(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_semantic_preapply_preview_draft_task(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
