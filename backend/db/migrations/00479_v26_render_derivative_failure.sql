-- +goose Up
-- Failure lifecycle is independent of successful media adoption.
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_failed_v26_render_derivative()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;
BEGIN
 SELECT x.tenant_id,x.id INTO candidate FROM photo.runs x
 WHERE x.v26_derivative_task_id IS NOT NULL AND x.kind='preview' AND x.status='running'
 AND x.graph_sealed AND NOT x.cancel_requested
 AND EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=x.tenant_id AND t.run_id=x.id AND t.state IN('failed','cancelled','skipped'))
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=x.tenant_id AND t.run_id=x.id AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 ORDER BY x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id FOR UPDATE;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=candidate.tenant_id FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id)<>2
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id AND state NOT IN('succeeded','failed','cancelled','skipped'))
 OR NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id AND state IN('failed','cancelled','skipped'))
 THEN RETURN false;END IF;
 UPDATE photo.runs SET status='failed',revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id;
 -- Release remains separately guarded by settled uploads/documents.
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_failed_v26_render_derivative() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_failed_v26_render_derivative() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_failed_v26_render_derivative() TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
