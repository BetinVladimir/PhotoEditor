-- +goose Up
-- Dedicated terminal quota sweep, independent of original detector deployment.
-- +goose StatementBegin
CREATE FUNCTION photo.release_next_v29_replacement_detector_quota()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;prior text;released boolean;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved'
 WHERE (r.config_snapshot ? 'v29_perspective_replacement_detection_jcs') AND r.kind='v2' AND r.status IN('completed','completed_with_errors','failed','cancelled')
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 AND NOT EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
 WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND u.state NOT IN('ready','cleaned'))
 AND NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.state NOT IN('ready','cleaned'))
 ORDER BY r.created_at,r.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 prior:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',candidate.tenant_id::text,true);
 released:=photo.release_v29_replacement_detector_output_quota(candidate.tenant_id,candidate.id);
 PERFORM set_config('app.tenant_id',coalesce(prior,''),true);
 RETURN released;
END $$;
ALTER FUNCTION photo.release_next_v29_replacement_detector_quota() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_next_v29_replacement_detector_quota() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.release_next_v29_replacement_detector_quota() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
