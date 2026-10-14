-- +goose Up
-- Completed-result canonical producer proof for frozen protection root and binary child.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_adoption_protection_origin(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_version uuid,p_source uuid,p_source_sha text,p_target boolean,p_root uuid,p_root_sha text,p_child uuid,p_child_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pins record; root_id uuid; root_sha text; project uuid;
BEGIN
 IF NOT photo.v27_semantic_adoption_source_ready(p_tenant,p_task,p_attempt,p_fence,p_result,p_version,p_source,p_source_sha,p_target) THEN RETURN false;END IF;
 SELECT * INTO pins FROM photo.v27_semantic_adoption_protection(p_tenant,p_task,p_attempt,p_fence,p_result);
 IF NOT FOUND THEN RETURN false;END IF;
 root_id:=CASE WHEN p_target THEN pins.target_artifact_id ELSE pins.source_artifact_id END;
 root_sha:=CASE WHEN p_target THEN pins.target_sha ELSE pins.source_sha END;
 IF root_id IS DISTINCT FROM p_root OR root_sha IS DISTINCT FROM p_root_sha
 OR p_child_sha IS NULL OR p_child_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 SELECT w.project_id INTO project FROM photo.v2_workflows w JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts root
 JOIN photo.artifacts child ON child.tenant_id=root.tenant_id AND child.id=p_child AND child.sha256=p_child_sha
 JOIN photo.tasks producer ON producer.tenant_id=root.tenant_id AND producer.id=root.created_by_task_id AND producer.id=child.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.task_id=producer.id AND result.id=producer.canonical_result_id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.runs run_row ON run_row.tenant_id=producer.tenant_id AND run_row.id=producer.run_id
 LEFT JOIN photo.v2_workflows wf ON wf.tenant_id=run_row.tenant_id AND wf.run_id=run_row.id
 WHERE root.tenant_id=p_tenant AND root.id=p_root AND root.sha256=p_root_sha
 AND root.state='ready' AND root.verified_at IS NOT NULL AND root.media_type='application/json'
 AND root.output_role IN('mask_protected_ref','defect_protection_ref','transformed_mask_ref')
 AND root.object_version IS NOT NULL AND root.object_version NOT IN('','null') AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
 AND child.state='ready' AND child.verified_at IS NOT NULL AND child.media_type='application/octet-stream' AND child.bytes>=32
 AND child.output_role IN('mask_protected','defect_protection','transformed_mask')
 AND child.object_version IS NOT NULL AND child.object_version NOT IN('','null') AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp())
 AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
 AND child.produced_attempt=producer.attempt AND child.produced_fence=producer.fencing_token AND producer.state='succeeded'
 AND (wf.project_id=project OR run_row.config_snapshot->>'project_id'=project::text)
 AND jsonb_typeof(result.outputs)='array'
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),jsonb_build_object('artifact_id',child.id,'sha256',child.sha256))
 AND (EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=p_tenant AND input.project_id=project AND input.task_id=producer.id AND input.contract='WorkingImage' AND input.artifact_id=p_source AND input.sha256=p_source_sha)
 OR EXISTS(SELECT 1 FROM photo.interactive_tasks input WHERE input.tenant_id=p_tenant AND input.task_id=producer.id AND input.source_artifact_id=p_source)));
END $$;
ALTER FUNCTION photo.v27_semantic_adoption_protection_origin(uuid,uuid,integer,bigint,uuid,uuid,uuid,text,boolean,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_adoption_protection_origin(uuid,uuid,integer,bigint,uuid,uuid,uuid,text,boolean,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_adoption_protection_origin(uuid,uuid,integer,bigint,uuid,uuid,uuid,text,boolean,uuid,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
