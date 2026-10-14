-- +goose Up
-- Reconciliation requests cancellation, then reports settlement in the same
-- authoritative transaction. This snapshot is not replacement admission.
-- +goose StatementBegin
CREATE FUNCTION photo.reconcile_v29_negative_invalidation(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_review uuid,p_decision text,p_plan jsonb,p_max_tasks integer,p_max_edges integer)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE affected_count integer;pending_affected integer;run_count integer;pending_runs integer;pending_run_tasks integer;
BEGIN
 IF photo.request_v29_negative_invalidation_cancel(p_tenant,p_project,p_actor,p_session,p_review,p_decision,p_plan,p_max_tasks,p_max_edges) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT count(*),count(*) FILTER(WHERE t.state NOT IN('succeeded','failed','cancelled','skipped'))
 INTO affected_count,pending_affected FROM photo.tasks t WHERE t.tenant_id=p_tenant
 AND t.id IN(SELECT value::uuid FROM jsonb_array_elements_text(p_plan->'affected_tasks'));
 SELECT count(*),count(*) FILTER(WHERE r.status NOT IN('completed','completed_with_errors','failed','cancelled'))
 INTO run_count,pending_runs FROM photo.runs r WHERE r.tenant_id=p_tenant
 AND r.id IN(SELECT value::uuid FROM jsonb_array_elements_text(p_plan->'consumer_runs'));
 -- Whole consumer runs use fail-fast cancellation, so unrelated live tasks in
 -- those runs must also settle before a replacement can be considered.
 SELECT count(*) INTO pending_run_tasks FROM photo.tasks t WHERE t.tenant_id=p_tenant
 AND t.run_id IN(SELECT value::uuid FROM jsonb_array_elements_text(p_plan->'consumer_runs'))
 AND t.state NOT IN('succeeded','failed','cancelled','skipped');
 IF affected_count<>jsonb_array_length(p_plan->'affected_tasks') OR run_count<>jsonb_array_length(p_plan->'consumer_runs') THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'affected_tasks',affected_count,'unsettled_affected_tasks',pending_affected,
 'consumer_runs',run_count,'unsettled_consumer_runs',pending_runs,'unsettled_consumer_tasks',pending_run_tasks,
 'settled',pending_affected=0 AND pending_runs=0 AND pending_run_tasks=0);
END $$;
ALTER FUNCTION photo.reconcile_v29_negative_invalidation(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reconcile_v29_negative_invalidation(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reconcile_v29_negative_invalidation(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
