-- +goose Up
-- One settled terminal reservation per pass; unfinished uploads do not starve later work.
-- +goose StatementBegin
CREATE FUNCTION photo.release_next_v26_render_derivative_quota() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved'
 WHERE r.v26_derivative_task_id IS NOT NULL AND r.kind='preview'
 AND r.status IN('completed','completed_with_errors','failed','cancelled')
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 AND NOT EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND u.state NOT IN('ready','cleaned'))
 AND NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.state='pending')
 ORDER BY r.created_at,r.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN photo.release_v26_render_derivative_quota(candidate.tenant_id,candidate.id);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_next_v26_render_derivative_quota() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_next_v26_render_derivative_quota() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_next_v26_render_derivative_quota() TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
