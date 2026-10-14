-- +goose Up
-- Donor adoption requires the proof-carrying dedicated scheduler entrypoint.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_donor;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_donor(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='portrait.open_eyes') THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_pre_donor(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
CREATE FUNCTION photo.adopt_v28_donor(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_inventory jsonb,p_digest text,p_evidence text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;actual jsonb;manual jsonb;origin record;project uuid;task_ids uuid[];
BEGIN
 IF p_evidence IS NULL OR octet_length(p_evidence) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 expected:=p_evidence::jsonb;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT w.project_id INTO project FROM photo.tasks t JOIN photo.v2_workflows w
 ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id WHERE t.tenant_id=p_tenant AND t.id=p_task;
 IF project IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT array_agg(DISTINCT id) INTO task_ids FROM (
 SELECT p_task id UNION ALL SELECT a.created_by_task_id FROM photo.tasks t JOIN photo.runs r
 ON r.tenant_id=t.tenant_id AND r.id=t.run_id JOIN photo.artifacts a ON a.tenant_id=r.tenant_id
 AND a.id=(r.v28_donor_descriptor_jcs::jsonb->'params'->'eyes_mask'->>'artifact_id')::uuid
 WHERE t.tenant_id=p_tenant AND t.id=p_task) ids WHERE id IS NOT NULL;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(
 SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(task_ids)) ORDER BY id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(task_ids) ORDER BY id FOR UPDATE;
 actual:=photo.v28_donor_adoption_evidence(p_tenant,p_task,p_attempt,p_fence,p_result);
 IF actual IS NULL OR actual IS DISTINCT FROM expected THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task
 AND attempt=p_attempt AND fencing_token=p_fence AND review_required) THEN RETURN false;END IF;
 manual:=actual->'manual';
 SELECT * INTO origin FROM photo.v28_donor_adoption_manual_origin(p_tenant,p_task,p_attempt,p_fence,p_result,
 (manual->'target_pixel'->>'artifact_id')::uuid,manual->'target_pixel'->>'sha256',
 (manual->'guard_pixel'->>'artifact_id')::uuid,manual->'guard_pixel'->>'sha256',
 (manual->'vectors'->>'artifact_id')::uuid,manual->'vectors'->>'sha256');
 IF NOT FOUND OR origin.root_id::text IS DISTINCT FROM manual->'root'->>'artifact_id'
 OR origin.root_sha IS DISTINCT FROM manual->'root'->>'sha256'
 OR origin.params_id::text IS DISTINCT FROM manual->'original_params'->>'artifact_id'
 OR origin.params_sha IS DISTINCT FROM manual->'original_params'->>'sha256' THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_pre_donor(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_v28_donor(uuid,uuid,integer,bigint,uuid,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v28_donor(uuid,uuid,integer,bigint,uuid,jsonb,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v28_donor(uuid,uuid,integer,bigint,uuid,jsonb,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
