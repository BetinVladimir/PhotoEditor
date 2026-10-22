-- +goose Up
-- Full retouch input provenance and quota lock gate, before task materialization.
-- Private physical readiness predicate. Live editor/source authority,
-- dependency provenance, quota and relational admission remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_documents_ready(p_tenant uuid,p_preview uuid,p_params jsonb,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task uuid;item record;artifact photo.artifacts%ROWTYPE;document photo.backend_documents%ROWTYPE;
 expected_role text;expected_id uuid;expected_sha text;
BEGIN
 IF photo.semantic_retouch_preview_transport_ready(p_preview,p_params,p_node,p_transport,p_inventory) IS DISTINCT FROM true THEN RETURN false;END IF;
 task:=(p_node->>'id')::uuid;
 IF NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_preview
 AND r.kind='preview' AND r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed
 AND r.config_snapshot->'semantic_retouch_preview'->'params'=p_params
 AND r.config_snapshot->'semantic_retouch_preview'->>'params_sha'=p_node->>'params_sha'
 AND r.v27_preapply_preview_profile=p_node->'execution_profile'
 AND r.config_snapshot->'semantic_retouch_preview'->'adapter_context_ref'=p_node->'execution_profile'->'retouch_context') THEN RETURN false;END IF;
 FOR item IN
 SELECT e->'ref' AS ref,e->>'contract' AS contract FROM jsonb_array_elements(p_inventory->'entries') e
 UNION ALL SELECT p_transport->'chunks'->0,'InputClosureChunk'
 UNION ALL SELECT p_transport->'manifest','InputClosureManifest'
 LOOP
 expected_id:=(item.ref->>'artifact_id')::uuid;expected_sha:=item.ref->>'sha256';
 SELECT * INTO artifact FROM photo.artifacts WHERE tenant_id=p_tenant AND id=expected_id;
 IF artifact.id IS NULL OR artifact.sha256 IS DISTINCT FROM expected_sha OR artifact.state<>'ready'
 OR artifact.verified_at IS NULL OR artifact.object_version IS NULL OR artifact.object_version IN('','null')
 OR artifact.bytes IS NULL OR artifact.bytes NOT BETWEEN 1 AND 9007199254740991
 OR (artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp())
 OR (item.contract='NpyArray' AND coalesce(artifact.media_type,'') NOT IN('application/x-npy','application/octet-stream'))
 OR (item.contract<>'NpyArray' AND artifact.media_type IS DISTINCT FROM 'application/json') THEN RETURN false;END IF;
 IF item.contract IN('Params','InputClosureChunk','InputClosureManifest','RetouchPreviewAdapterContext') THEN
 expected_role:=CASE item.contract WHEN 'Params' THEN 'task_params'
 WHEN 'InputClosureChunk' THEN 'v2_input_chunk_0' WHEN 'RetouchPreviewAdapterContext' THEN 'retouch_adapter_context' ELSE 'v2_input_manifest' END;
 SELECT * INTO document FROM photo.backend_documents d WHERE d.tenant_id=p_tenant
 AND d.run_id=p_preview AND d.owner_id=task AND d.purpose=expected_role;
 IF document.id IS NULL OR document.state<>'ready' OR document.artifact_id IS DISTINCT FROM expected_id
 OR document.sha256 IS DISTINCT FROM expected_sha OR document.bytes IS DISTINCT FROM artifact.bytes
 OR document.bucket IS DISTINCT FROM artifact.bucket OR document.object_version IS DISTINCT FROM artifact.object_version
 OR artifact.output_role IS DISTINCT FROM expected_role
 OR artifact.object_key IS DISTINCT FROM 'tenants/'||p_tenant||'/runs/'||p_preview||'/documents/'||expected_id THEN RETURN false;END IF;
 END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_documents_ready(uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_documents_ready(uuid,uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Private pre-lease protection provenance. Caller must separately lock current
-- authenticated preview/target and validate exact MaskRef/binary content.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_protection_ready(p_tenant uuid,p_project uuid,
 p_params jsonb,p_binary uuid,p_binary_sha text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE edit jsonb:=p_params;p_root uuid;p_root_sha text;
BEGIN
 IF jsonb_typeof(edit) IS DISTINCT FROM 'object' OR p_project IS NULL
 OR p_binary IS NULL OR p_binary='00000000-0000-0000-0000-000000000000'::uuid
 OR p_binary_sha IS NULL OR p_binary_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 p_root:=(edit->'protection'->>'artifact_id')::uuid;p_root_sha:=edit->'protection'->>'sha256';
 IF p_root IS NULL OR p_root='00000000-0000-0000-0000-000000000000'::uuid
 OR p_root=p_binary OR p_root_sha IS NULL OR p_root_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts candidate
 JOIN photo.tasks producer ON producer.tenant_id=candidate.tenant_id AND producer.id=candidate.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.task_id=producer.id
 AND result.id=producer.canonical_result_id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.runs original ON original.tenant_id=producer.tenant_id AND original.id=producer.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=original.tenant_id AND w.run_id=original.id AND w.project_id=p_project
 JOIN photo.semantic_intents i ON i.tenant_id=w.tenant_id AND i.workflow_id=w.id
 JOIN photo.semantic_targets target ON target.tenant_id=i.tenant_id AND target.intent_id=i.id AND target.id=(edit->>'target_id')::uuid
 WHERE w.id::text=edit->>'workflow_id' AND i.id::text=edit->>'intent_id'
 AND candidate.tenant_id=p_tenant AND candidate.id=(edit->'candidate'->>'artifact_id')::uuid
 AND candidate.sha256=edit->'candidate'->>'sha256' AND candidate.produced_attempt=producer.attempt
 AND candidate.produced_fence=producer.fencing_token AND producer.state='succeeded'
 AND producer.operation='semantic.transfer' AND producer.operation_version='1'
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',candidate.id,'sha256',candidate.sha256))
 AND jsonb_typeof(original.config_snapshot->'v27_semantic'->'nodes')='array'
 AND (SELECT count(*) FROM jsonb_array_elements(original.config_snapshot->'v27_semantic'->'nodes') n
 WHERE n->>'task_id'=producer.id::text AND n->'target_protection'=jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha))=1
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=p_tenant AND input.project_id=p_project
 AND input.task_id=producer.id AND input.artifact_id=p_root AND input.sha256=p_root_sha AND input.contract='MaskRef')) THEN RETURN false;END IF;
 RETURN photo.semantic_protection_producer_ready(p_tenant,p_project,(edit->'source'->'source'->>'artifact_id')::uuid,
 edit->'source'->'source'->>'sha256',p_root,p_root_sha,p_binary,p_binary_sha);
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_protection_ready(uuid,uuid,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_protection_ready(uuid,uuid,jsonb,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
