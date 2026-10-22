-- +goose Up
-- Private pre-lease protection provenance. Caller must separately lock current
-- authenticated preview/target and validate exact MaskRef/binary content.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_protection_ready(p_tenant uuid,p_project uuid,
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
ALTER FUNCTION photo.semantic_preapply_preview_protection_ready(uuid,uuid,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_protection_ready(uuid,uuid,jsonb,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
