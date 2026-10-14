-- +goose Up
-- Relight results require a dedicated proof-carrying scheduler commit.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_relight;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_relight(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='image.relight') THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_pre_relight(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
CREATE FUNCTION photo.adopt_v28_relight(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_inventory jsonb,p_digest text,p_evidence text,p_body text,p_depth text,p_subject text,p_guard text,p_subject_root text,p_guard_root text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;actual jsonb;project uuid;run uuid;
BEGIN
 IF p_evidence IS NULL OR octet_length(p_evidence) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 expected:=p_evidence::jsonb;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT w.project_id,t.run_id INTO project,run FROM photo.tasks t JOIN photo.v2_workflows w
 ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id WHERE t.tenant_id=p_tenant AND t.id=p_task;
 IF project IS NULL OR run IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id IN (
  SELECT t.run_id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND (t.id=p_task OR t.id IN (
   SELECT a.created_by_task_id FROM photo.artifacts a JOIN photo.v2_task_inputs i
   ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id WHERE i.tenant_id=p_tenant AND i.task_id=p_task)))
 ORDER BY r.id FOR UPDATE;
 PERFORM t.id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND (t.id=p_task OR t.id IN (
  SELECT a.created_by_task_id FROM photo.artifacts a JOIN photo.v2_task_inputs i
  ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id WHERE i.tenant_id=p_tenant AND i.task_id=p_task))
 ORDER BY t.id FOR UPDATE;
 actual:=photo.v28_relight_adoption_evidence(p_tenant,p_task,p_attempt,p_fence,p_result,p_body,p_depth,p_subject,p_guard,p_subject_root,p_guard_root);
 IF actual IS NULL OR actual IS DISTINCT FROM expected THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task AND attempt=p_attempt AND fencing_token=p_fence AND review_required) THEN RETURN false;END IF;
 -- Generic closure/link publication remains one transaction; bypass only the
 -- relight dispatch guard after the dedicated current evidence re-lock.
 RETURN photo.adopt_v2_plan_pre_relight(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
