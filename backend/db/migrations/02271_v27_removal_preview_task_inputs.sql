-- +goose Up
-- Removal task materialization and scoped input registration.
-- Latest shared guard retains workflow, Apply, exposure and shown Apply paths.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_removal_preview_run_current(p_tenant uuid,p_preview uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;config jsonb;e jsonb;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_preview;
 config:=r.config_snapshot->'semantic_removal_preview';e:=config->'params';
 IF r.id IS NULL OR r.kind<>'preview' OR r.cancel_requested
 OR jsonb_typeof(config) IS DISTINCT FROM 'object' OR jsonb_typeof(e) IS DISTINCT FROM 'object'
 OR config->>'actor_id' IS DISTINCT FROM r.created_by::text THEN RETURN false;END IF;
 RETURN photo.semantic_preapply_preview_current(p_tenant,(e->>'intent_id')::uuid,(e->>'target_id')::uuid,
 (config->>'actor_id')::uuid,(config->>'session_id')::uuid,(e->>'workflow_revision')::bigint,
 (e->>'target_revision')::bigint,e->'plan'->>'sha256',e->'candidate'->>'sha256');
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_removal_preview_run_current(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_removal_preview_run_current(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
-- Private transaction component. Exact inventory registration, seal and
-- activation must follow in the same transaction or the caller must roll back.
-- Never changes the photographer's proposed target or creates an Apply child.
-- +goose StatementBegin
CREATE FUNCTION photo.create_semantic_removal_preview_draft_task(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,
 p_body text,p_sha text,p_candidate_body text,p_intent_body text,p_source_body text,
 p_reference_body text,p_mask_body text,p_transform_body text,p_protection_body text,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task_id uuid;existing photo.tasks%ROWTYPE;
BEGIN
 IF photo.semantic_removal_preview_admission_inputs_ready(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_candidate_body,p_intent_body,p_source_body,p_reference_body,p_mask_body,p_transform_body,
 p_protection_body,p_node,p_transport,p_inventory) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INPUTS_UNAVAILABLE' USING ERRCODE='23514';END IF;
 task_id:=(p_node->>'id')::uuid;
 SELECT * INTO existing FROM photo.tasks WHERE id=task_id FOR UPDATE;
 IF FOUND THEN
 IF existing.tenant_id IS DISTINCT FROM p_tenant OR existing.run_id IS DISTINCT FROM p_preview
 OR existing.node_key IS DISTINCT FROM p_node->>'node_key'
 OR existing.operation IS DISTINCT FROM 'semantic.preview_remove_object'
 OR existing.operation_version IS DISTINCT FROM '1' OR existing.resource_class IS DISTINCT FROM 'gpu'
 OR existing.state IS DISTINCT FROM 'blocked' OR existing.attempt IS DISTINCT FROM 0
 OR existing.cancel_requested IS DISTINCT FROM false OR existing.inputs IS DISTINCT FROM '[]'::jsonb
 OR existing.params_artifact_id IS DISTINCT FROM (p_node->>'params_artifact_id')::uuid
 OR existing.input_digest IS DISTINCT FROM p_node->>'input_digest'
 OR existing.execution_profile IS DISTINCT FROM p_node->'execution_profile' THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_TASK_CONFLICT' USING ERRCODE='40001';END IF;
 ELSE
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,
 state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(task_id,p_tenant,p_preview,p_node->>'node_key','semantic.preview_remove_object','1','gpu',
 'blocked','[]'::jsonb,(p_node->>'params_artifact_id')::uuid,p_node->>'input_digest',p_node->'execution_profile');
 END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_preview AND id<>task_id)
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant
 AND (run_id=p_preview OR prerequisite_id=task_id OR dependent_id=task_id)) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_GRAPH_CONFLICT' USING ERRCODE='23514';END IF;
 RETURN task_id;
END $$;
ALTER FUNCTION photo.create_semantic_removal_preview_draft_task(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_semantic_removal_preview_draft_task(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Preview input registration scope only; full provenance admission remains
-- mandatory in the private materialization transaction. Existing scopes retained.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_removal_preview_input_scope(p_tenant uuid,p_project uuid,p_task uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.semantic_intents i ON i.tenant_id=r.tenant_id
 AND i.id::text=r.config_snapshot->'semantic_removal_preview'->'params'->>'intent_id'
 JOIN photo.semantic_targets target ON target.tenant_id=i.tenant_id AND target.intent_id=i.id
 AND target.id::text=r.config_snapshot->'semantic_removal_preview'->'params'->>'target_id'
 JOIN photo.projects project ON project.tenant_id=i.tenant_id AND project.id=i.project_id AND project.archived_at IS NULL
 JOIN photo.tenants tenant ON tenant.id=i.tenant_id AND tenant.status='active'
 JOIN photo.artifacts params ON params.tenant_id=t.tenant_id AND params.id=t.params_artifact_id
 JOIN photo.backend_documents d ON d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.owner_id=t.id
 AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=params.id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND i.project_id=p_project
 AND target.project_id=p_project AND target.state='proposed' AND target.child_run_id IS NULL AND target.batch_id IS NULL
 AND r.kind='preview' AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested
 AND t.state='blocked' AND t.attempt=0 AND NOT t.cancel_requested
 AND t.operation='semantic.preview_remove_object' AND t.operation_version='1' AND t.resource_class='gpu'
 AND t.inputs='[]'::jsonb AND t.execution_profile=r.v27_preapply_preview_profile
 AND NOT(t.execution_profile ? 'v2_input_manifest')
 AND t.id=photo.semantic_uuid_v5(r.id,'semantic.preview_remove_object:'||target.id::text)
 AND t.node_key='semantic.preview_remove_object:'||target.id::text
 AND t.params_artifact_id=photo.semantic_uuid_v5(t.id,'Params')
 AND params.sha256=r.config_snapshot->'semantic_removal_preview'->>'params_sha'
 AND params.state='ready' AND params.verified_at IS NOT NULL
 AND params.object_version IS NOT NULL AND params.object_version NOT IN('','null')
 AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
 AND d.sha256=params.sha256 AND d.bytes=params.bytes AND d.bucket=params.bucket AND d.object_version=params.object_version
 AND r.created_by::text=r.config_snapshot->'semantic_removal_preview'->>'actor_id'
 AND photo.semantic_removal_preview_run_current(p_tenant,r.id) IS TRUE);
$$;
ALTER FUNCTION photo.semantic_removal_preview_input_scope(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_removal_preview_input_scope(uuid,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Private same-transaction registration. No sealing, task readiness or dispatch.
-- Partial or altered previous registration is a conflict, never silently repaired.
-- +goose StatementBegin
CREATE FUNCTION photo.register_semantic_removal_preview_inputs(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,
 p_body text,p_sha text,p_candidate_body text,p_intent_body text,p_source_body text,
 p_reference_body text,p_mask_body text,p_transform_body text,p_protection_body text,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE owner uuid;project uuid;expected jsonb;actual jsonb;entry jsonb;total integer;
BEGIN
 owner:=photo.create_semantic_removal_preview_draft_task(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_candidate_body,p_intent_body,p_source_body,p_reference_body,p_mask_body,p_transform_body,
 p_protection_body,p_node,p_transport,p_inventory);
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF photo.semantic_removal_preview_input_scope(p_tenant,project,owner) IS DISTINCT FROM true THEN
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
ALTER FUNCTION photo.register_semantic_removal_preview_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_semantic_removal_preview_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.guard_v2_artifact_link() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE artifact photo.artifacts%ROWTYPE; parent_ok boolean;
BEGIN
 SELECT * INTO artifact FROM photo.artifacts a WHERE a.tenant_id=NEW.tenant_id AND a.id=NEW.artifact_id;
 IF artifact.id IS NULL OR artifact.sha256<>NEW.sha256 OR artifact.state<>'ready'
  OR artifact.object_version IS NULL OR artifact.verified_at IS NULL
  OR (artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp()) THEN
  RAISE EXCEPTION 'V2_ARTIFACT_UNVERIFIED' USING ERRCODE='23514';
 END IF;
 CASE TG_TABLE_NAME
  WHEN 'v2_documents' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_document_streams s
    WHERE s.tenant_id=NEW.tenant_id AND s.project_id=NEW.project_id AND s.id=NEW.stream_id)
    INTO parent_ok;
  WHEN 'v2_document_refs' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_documents d
    WHERE d.tenant_id=NEW.tenant_id AND d.project_id=NEW.project_id AND d.id=NEW.document_id)
    INTO parent_ok;
  WHEN 'v2_artifact_links' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_workflows w
    WHERE w.tenant_id=NEW.tenant_id AND w.project_id=NEW.project_id AND w.id=NEW.workflow_id)
    INTO parent_ok;
  WHEN 'v2_task_inputs' THEN
   SELECT EXISTS(SELECT 1 FROM photo.tasks t
    JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
    WHERE t.tenant_id=NEW.tenant_id AND t.id=NEW.task_id AND w.project_id=NEW.project_id)
    OR photo.semantic_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_preapply_preview_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_removal_preview_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_shown_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    INTO parent_ok;
  ELSE
   RAISE EXCEPTION 'V2_ARTIFACT_LINK_TABLE_INVALID' USING ERRCODE='23514';
 END CASE;
 IF parent_ok IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'V2_ARTIFACT_LINK_SCOPE_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
