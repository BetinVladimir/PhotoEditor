-- +goose Up
-- Atomic inventory retention after repeated shown-copy proof under locks.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_shown_exposure(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text,p_params uuid,p_sha text,p_body text,
 p_root uuid,p_root_sha text,p_output_body text,p_shown_body text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;project uuid;task_ids uuid[];e jsonb;accepted boolean;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_output_body IS NULL OR octet_length(p_output_body) NOT BETWEEN 2 AND 1048576
 OR p_shown_body IS NULL OR octet_length(p_shown_body) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 e:=p_body::jsonb;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL OR t.operation IS DISTINCT FROM 'semantic.adopt_shown_exposure'
 OR t.operation_version IS DISTINCT FROM '1' THEN RETURN false;END IF;
 SELECT (r.config_snapshot->>'project_id')::uuid INTO project FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=t.run_id;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT array_agg(DISTINCT id ORDER BY id) INTO task_ids FROM (
 SELECT p_task AS id UNION ALL SELECT a.created_by_task_id FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND (a.id IN(p_root,(p_output_body::jsonb->'pixels'->>'artifact_id')::uuid)
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=a.id))
 UNION ALL SELECT (e->>'preview_task_id')::uuid) ids WHERE id IS NOT NULL;
 PERFORM r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id IN(
 SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(task_ids)) ORDER BY r.id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(task_ids) ORDER BY id FOR UPDATE;
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=p_tenant AND id=(e->>'target_id')::uuid FOR UPDATE;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND (a.id IN(p_params,p_root,
 (p_output_body::jsonb->'pixels'->>'artifact_id')::uuid) OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=a.id)) ORDER BY a.id FOR SHARE;
 IF photo.shown_exposure_output_bound(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_sha,p_body,
 p_root,p_root_sha,p_output_body,p_shown_body) IS DISTINCT FROM true THEN RETURN false;END IF;
 accepted:=photo.adopt_shown_exposure_inventory(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
 IF accepted AND photo.shown_exposure_output_bound(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_sha,p_body,
 p_root,p_root_sha,p_output_body,p_shown_body) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_ADOPTION_STALE' USING ERRCODE='23514';END IF;
 RETURN accepted;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_shown_exposure(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_shown_exposure(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.adopt_shown_exposure(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
