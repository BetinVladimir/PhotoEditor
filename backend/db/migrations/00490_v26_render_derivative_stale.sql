-- +goose Up
-- Cancellation only: object cleanup and guarded quota release stay separate.
-- +goose StatementBegin
CREATE FUNCTION photo.cancel_stale_v26_render_derivative()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;has_source boolean;has_quota boolean;
BEGIN
 SELECT x.tenant_id,x.id INTO candidate FROM photo.runs x JOIN photo.tenants n ON n.id=x.tenant_id AND n.status='active'
 WHERE x.v26_derivative_task_id IS NOT NULL AND x.kind='preview' AND x.status IN('draft','running') AND NOT x.cancel_requested
 AND (NOT EXISTS(SELECT 1 FROM photo.v26_render_derivative_source(x.tenant_id,x.v26_derivative_task_id))
 OR EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=x.tenant_id AND q.holder_type='run' AND q.holder_id=x.id AND q.state='reserved' AND q.expires_at<=clock_timestamp())
 OR (x.created_at<=clock_timestamp()-interval '1 hour' AND NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=x.tenant_id AND q.holder_type='run' AND q.holder_id=x.id AND q.state='reserved')))
 ORDER BY x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=candidate.tenant_id FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR r.status NOT IN('draft','running') THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=r.tenant_id AND id=(r.config_snapshot->>'project_id')::uuid FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=r.tenant_id AND id=(r.config_snapshot->>'source_run_id')::uuid FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=r.tenant_id AND id=r.v26_derivative_task_id FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=r.tenant_id AND id=(r.config_snapshot->'root'->>'artifact_id')::uuid FOR SHARE;
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=r.tenant_id AND holder_type='run' AND holder_id=r.id FOR UPDATE;
 has_source:=EXISTS(SELECT 1 FROM photo.v26_render_derivative_source(r.tenant_id,r.v26_derivative_task_id));
 has_quota:=EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=r.tenant_id AND holder_type='run' AND holder_id=r.id AND state='reserved' AND expires_at>clock_timestamp());
 IF has_source AND has_quota THEN RETURN false;END IF;
 IF has_source AND NOT EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=r.tenant_id AND holder_type='run' AND holder_id=r.id AND state='reserved')
 AND r.created_at>clock_timestamp()-interval '1 hour' THEN RETURN false;END IF;
 UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id;
 UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=r.tenant_id AND run_id=r.id AND state NOT IN('succeeded','failed','cancelled','skipped');
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.cancel_stale_v26_render_derivative() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.cancel_stale_v26_render_derivative() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.cancel_stale_v26_render_derivative() TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
