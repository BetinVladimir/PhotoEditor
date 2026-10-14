-- +goose Up
-- Cleaning documents still own in-flight object cleanup; retain run quota until cleaned.
-- +goose StatementBegin
CREATE FUNCTION photo.release_v29_perspective_quality_quota(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v29_perspective_quality_task_id IS NULL OR r.kind<>'preview'
  OR r.status NOT IN('completed','completed_with_errors','failed','cancelled') THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 OR EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND u.state NOT IN('ready','cleaned'))
 OR EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.state NOT IN('ready','cleaned')) THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN true;END IF;
 IF q.state='released' AND q.bytes=0 THEN RETURN true;END IF;
 IF q.state<>'reserved' OR q.principal_type<>'user' OR q.principal_id<>r.created_by THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DERIVATIVE_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
 UPDATE photo.quota_reservations SET state='released',bytes=0,expires_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=q.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_v29_perspective_quality_quota(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_v29_perspective_quality_quota(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_v29_perspective_quality_quota(uuid,uuid) TO photo_scheduler;
-- One settled terminal reservation per pass; unfinished uploads do not starve later work.
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
