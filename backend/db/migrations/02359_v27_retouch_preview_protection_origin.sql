-- +goose Up
-- Retouch preview canonical protection provenance.
-- Requires admit_semantic_retouch_preview from the retouch admission migration;
-- operation registration remains disabled until the complete SQL set is installed.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_protection_origin(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text,p_root uuid,p_root_sha text,p_binary uuid,p_binary_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE edit jsonb; project uuid;
BEGIN
 IF photo.admit_semantic_retouch_preview(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 edit:=p_body::jsonb;
 IF p_root IS NULL OR p_binary IS NULL OR p_root=p_binary
 OR p_root_sha IS NULL OR p_root_sha !~ '^[0-9a-f]{64}$'
 OR p_binary_sha IS NULL OR p_binary_sha !~ '^[0-9a-f]{64}$'
 OR edit->'protection' IS DISTINCT FROM jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha)
 THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.artifact_id=p_root AND i.sha256=p_root_sha AND i.contract='MaskRef')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.artifact_id=p_binary AND i.sha256=p_binary_sha AND i.contract='NpyArray') THEN RETURN false;END IF;
 SELECT t.project_id INTO project FROM photo.semantic_targets t WHERE t.tenant_id=p_tenant AND t.id=(edit->>'target_id')::uuid;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts candidate
 JOIN photo.tasks producer ON producer.tenant_id=candidate.tenant_id AND producer.id=candidate.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.task_id=producer.id
 AND result.id=producer.canonical_result_id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.runs original ON original.tenant_id=producer.tenant_id AND original.id=producer.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=original.tenant_id AND w.run_id=original.id AND w.project_id=project
 JOIN photo.semantic_intents i ON i.tenant_id=w.tenant_id AND i.workflow_id=w.id
 JOIN photo.semantic_targets target ON target.tenant_id=i.tenant_id AND target.intent_id=i.id AND target.id=(edit->>'target_id')::uuid
 WHERE candidate.tenant_id=p_tenant AND candidate.id=(edit->'candidate'->>'artifact_id')::uuid
 AND candidate.sha256=edit->'candidate'->>'sha256' AND candidate.produced_attempt=producer.attempt
 AND candidate.produced_fence=producer.fencing_token AND producer.state='succeeded'
 AND producer.operation='semantic.transfer' AND producer.operation_version='1'
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',candidate.id,'sha256',candidate.sha256))
 AND jsonb_typeof(original.config_snapshot->'v27_semantic'->'nodes')='array'
 AND (SELECT count(*) FROM jsonb_array_elements(original.config_snapshot->'v27_semantic'->'nodes') n
 WHERE n->>'task_id'=producer.id::text AND n->'target_protection'=jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha))=1
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=p_tenant AND input.project_id=project
 AND input.task_id=producer.id AND input.artifact_id=p_root AND input.sha256=p_root_sha AND input.contract='MaskRef')) THEN RETURN false;END IF;
 RETURN photo.semantic_protection_producer_ready(p_tenant,project,(edit->'source'->'source'->>'artifact_id')::uuid,
 edit->'source'->'source'->>'sha256',p_root,p_root_sha,p_binary,p_binary_sha);
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text) FROM PUBLIC,photo_api,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_retouch_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text) TO photo_worker;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
