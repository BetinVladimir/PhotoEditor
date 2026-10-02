-- +goose Up
ALTER TABLE photo.output_uploads ADD COLUMN cleanup_token uuid;
ALTER TABLE photo.output_uploads ADD COLUMN cleanup_lease_until timestamptz;
ALTER TABLE photo.output_uploads ADD COLUMN cleanup_after timestamptz NOT NULL DEFAULT clock_timestamp()+interval '3 hours';
-- Cleaned tombstones are retained and revisited: a late S3 completion after a
-- network timeout must still be removed, without decrementing quota twice.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_output_cleanup()
RETURNS TABLE(upload_id uuid,cleanup_token uuid,tenant_id uuid,task_id uuid,artifact_id uuid,attempt integer,fence bigint,bucket text,object_key text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate uuid; task_ref uuid; t photo.tasks%ROWTYPE; u photo.output_uploads%ROWTYPE; token uuid:=gen_random_uuid();
BEGIN
 SELECT x.id INTO candidate FROM photo.output_uploads x JOIN photo.tasks task ON task.id=x.task_id AND task.tenant_id=x.tenant_id
 WHERE x.state IN ('pending','cleaning','cleaned') AND x.cleanup_after<=clock_timestamp()
 AND (x.cleanup_lease_until IS NULL OR x.cleanup_lease_until<=clock_timestamp())
 AND NOT (task.state='running' AND task.attempt=x.attempt AND task.fencing_token=x.fence AND task.lease_until>clock_timestamp() AND task.deadline>clock_timestamp())
 ORDER BY x.cleanup_after,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT x.task_id INTO task_ref FROM photo.output_uploads x WHERE x.id=candidate;
 t:=photo.lock_task(task_ref);
 SELECT * INTO u FROM photo.output_uploads x WHERE x.id=candidate AND x.task_id=t.id AND x.tenant_id=t.tenant_id
 AND x.state IN ('pending','cleaning','cleaned') AND x.cleanup_after<=clock_timestamp()
 AND (x.cleanup_lease_until IS NULL OR x.cleanup_lease_until<=clock_timestamp()) ORDER BY x.cleanup_after,x.id LIMIT 1 FOR UPDATE;
 IF NOT FOUND OR (t.state='running' AND t.attempt=u.attempt AND t.fencing_token=u.fence AND t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp())
 OR EXISTS(SELECT FROM photo.artifacts a WHERE a.id=u.artifact_id AND a.tenant_id=u.tenant_id) THEN RETURN; END IF;
 UPDATE photo.output_uploads SET state=CASE WHEN state='cleaned' THEN 'cleaned' ELSE 'cleaning' END,
 cleanup_token=token,cleanup_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=u.id;
 RETURN QUERY SELECT u.id,token,u.tenant_id,u.task_id,u.artifact_id,u.attempt,u.fence,u.bucket,u.object_key;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_output_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_output_cleanup() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_output_cleanup() TO photo_maintenance;
-- +goose StatementBegin
CREATE FUNCTION photo.finish_output_cleanup(p_upload uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.output_uploads%ROWTYPE; t photo.tasks%ROWTYPE; u photo.output_uploads%ROWTYPE;
BEGIN
 SELECT * INTO hint FROM photo.output_uploads WHERE id=p_upload;
 IF NOT FOUND THEN RETURN false; END IF;
 t:=photo.lock_task(hint.task_id);
 SELECT * INTO u FROM photo.output_uploads WHERE id=p_upload FOR UPDATE;
 IF p_token IS NULL OR u.cleanup_lease_until IS NULL OR u.state NOT IN ('cleaning','cleaned') OR u.cleanup_token IS DISTINCT FROM p_token OR u.cleanup_lease_until<=clock_timestamp()
 OR EXISTS(SELECT FROM photo.artifacts a WHERE a.id=u.artifact_id AND a.tenant_id=u.tenant_id) THEN RETURN false; END IF;
 IF u.state='cleaning' THEN
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-u.bytes WHERE tenant_id=u.tenant_id AND reserved_bytes>=u.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'UPLOAD_RESERVATION_INCONSISTENT' USING ERRCODE='23514'; END IF;
 END IF;
 UPDATE photo.output_uploads SET state='cleaned',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_after=clock_timestamp()+interval '24 hours' WHERE id=u.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_output_cleanup(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_output_cleanup(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_output_cleanup(uuid,uuid) TO photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
