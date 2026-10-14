-- +goose Up
CREATE TABLE photo.v29_negative_skip_closures(
 tenant_id uuid NOT NULL,
 review_id uuid NOT NULL,
 origin_task_id uuid NOT NULL,
 decision_hash text NOT NULL CHECK(decision_hash ~ '^[0-9a-f]{64}$'),
 plan jsonb NOT NULL CHECK(jsonb_typeof(plan)='object' AND plan->>'route'='skip' AND pg_column_size(plan)<=1048576),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,review_id),
 FOREIGN KEY(tenant_id,review_id) REFERENCES photo.reviews(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,origin_task_id) REFERENCES photo.tasks(tenant_id,id) ON DELETE CASCADE
);
ALTER TABLE photo.v29_negative_skip_closures ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v29_negative_skip_closures FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v29_negative_skip_closures TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,DELETE ON photo.v29_negative_skip_closures TO photo_fn_owner;
CREATE TRIGGER v29_negative_skip_closures_immutable BEFORE UPDATE ON photo.v29_negative_skip_closures FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
-- +goose StatementBegin
CREATE FUNCTION photo.close_v29_negative_skipped_dependencies(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_review uuid,p_hash text,p_plan jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE settlement jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_plan->>'route' IS DISTINCT FROM 'skip' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 settlement:=photo.reconcile_v29_negative_invalidation(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,4096,65536);
 IF settlement IS NULL OR settlement->>'settled' IS DISTINCT FROM 'true' THEN RETURN false;END IF;
 INSERT INTO photo.v29_negative_skip_closures(tenant_id,review_id,origin_task_id,decision_hash,plan)
 VALUES(p_tenant,p_review,(p_plan->>'origin_task_id')::uuid,p_hash,p_plan) ON CONFLICT DO NOTHING;
 RETURN EXISTS(SELECT 1 FROM photo.v29_negative_skip_closures c WHERE c.tenant_id=p_tenant AND c.review_id=p_review
 AND c.origin_task_id=(p_plan->>'origin_task_id')::uuid AND c.decision_hash=p_hash AND c.plan=p_plan);
END $$;
ALTER FUNCTION photo.close_v29_negative_skipped_dependencies(uuid,uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.close_v29_negative_skipped_dependencies(uuid,uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.close_v29_negative_skipped_dependencies(uuid,uuid,uuid,uuid,uuid,text,jsonb) TO photo_scheduler;

CREATE FUNCTION photo.v29_negative_skip_closure_ready(p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.v29_negative_skip_closures c
 JOIN photo.v29_negative_review_selection(p_tenant,p_task) s ON s.review_id=c.review_id AND s.decision_hash=c.decision_hash AND s.route='skip'
 WHERE c.tenant_id=p_tenant AND c.origin_task_id=p_task AND c.plan->>'origin_task_id'=p_task::text
 AND (SELECT count(*) FROM photo.v29_negative_invalidations i WHERE i.tenant_id=p_tenant AND i.review_id=c.review_id
 AND i.origin_task_id=p_task AND i.decision_hash=c.decision_hash AND i.route='skip')=jsonb_array_length(c.plan->'affected_tasks')
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text(c.plan->'affected_tasks') id
 WHERE NOT EXISTS(SELECT 1 FROM photo.v29_negative_invalidations i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 WHERE i.tenant_id=p_tenant AND i.task_id=id::uuid AND i.review_id=c.review_id AND i.origin_task_id=p_task
 AND i.decision_hash=c.decision_hash AND i.route='skip' AND t.state IN('succeeded','failed','cancelled','skipped')))
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text(c.plan->'consumer_runs') id
 WHERE NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=id::uuid
 AND r.status IN('completed','completed_with_errors','failed','cancelled')
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state NOT IN('succeeded','failed','cancelled','skipped')))))
$$;
ALTER FUNCTION photo.v29_negative_skip_closure_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_skip_closure_ready(uuid,uuid) FROM PUBLIC;
ALTER FUNCTION photo.v29_negative_review_completion_ready(uuid,uuid) RENAME TO v29_negative_review_completion_ready_pre_skip_closure;
REVOKE ALL ON FUNCTION photo.v29_negative_review_completion_ready_pre_skip_closure(uuid,uuid) FROM PUBLIC;
CREATE FUNCTION photo.v29_negative_review_completion_ready(p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v29_negative_review_completion_ready_pre_skip_closure(p_tenant,p_task)
 OR photo.v29_negative_skip_closure_ready(p_tenant,p_task)
$$;
ALTER FUNCTION photo.v29_negative_review_completion_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_review_completion_ready(uuid,uuid) FROM PUBLIC;
-- Extend only the existing explicit, object-cleanup-gated tenant purge list.
DO $$ DECLARE definition text;anchor text:='''v29_negative_invalidations'',';BEGIN
 definition:=pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure);
 IF strpos(definition,anchor)=0 OR strpos(definition,'v29_negative_skip_closures')>0 THEN RAISE EXCEPTION 'NEGATIVE_SKIP_PURGE_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,anchor,'''v29_negative_skip_closures'','||anchor);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
