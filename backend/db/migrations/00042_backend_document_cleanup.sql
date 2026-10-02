-- +goose Up
ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_state_check;
ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_version_state;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_state_check CHECK(state IN ('pending','ready','cleaning','cleaned'));
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_version_state CHECK(
 (state IN ('pending','cleaning','cleaned') AND object_version IS NULL AND completed_at IS NULL) OR
 (state='ready' AND object_version IS NOT NULL AND object_version<>'' AND object_version<>'null' AND completed_at IS NOT NULL));
ALTER TABLE photo.backend_documents ADD COLUMN cleanup_token uuid;
ALTER TABLE photo.backend_documents ADD COLUMN cleanup_lease_until timestamptz;
ALTER TABLE photo.backend_documents ADD COLUMN cleanup_after timestamptz NOT NULL DEFAULT clock_timestamp()+interval '3 hours';
CREATE INDEX backend_documents_cleanup ON photo.backend_documents(cleanup_after,id) WHERE state IN ('pending','cleaning','cleaned');

-- +goose StatementBegin
CREATE FUNCTION photo.claim_document_cleanup()
RETURNS TABLE(document_id uuid,cleanup_token uuid,tenant_id uuid,run_id uuid,artifact_id uuid,bucket text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.backend_documents%ROWTYPE; d photo.backend_documents%ROWTYPE; r photo.runs%ROWTYPE; fence uuid:=gen_random_uuid();
BEGIN
 SELECT b.* INTO hint FROM photo.backend_documents b JOIN photo.runs rr ON rr.tenant_id=b.tenant_id AND rr.id=b.run_id
 WHERE b.state IN ('pending','cleaning','cleaned') AND b.cleanup_after<=clock_timestamp()
 AND (b.cleanup_lease_until IS NULL OR b.cleanup_lease_until<=clock_timestamp())
 AND (rr.cancel_requested OR rr.status IN ('cancelled','completed','completed_with_errors','failed'))
 ORDER BY b.cleanup_after,b.id LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM u.id FROM photo.tenant_usage u WHERE u.tenant_id=hint.tenant_id FOR UPDATE;
 SELECT rr.* INTO r FROM photo.runs rr WHERE rr.id=hint.run_id AND rr.tenant_id=hint.tenant_id FOR UPDATE;
 IF NOT FOUND OR NOT(r.cancel_requested OR r.status IN ('cancelled','completed','completed_with_errors','failed')) THEN RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.id=hint.id FOR UPDATE;
 IF d.state NOT IN ('pending','cleaning','cleaned') OR d.cleanup_after>clock_timestamp()
 OR (d.cleanup_lease_until IS NOT NULL AND d.cleanup_lease_until>clock_timestamp())
 OR EXISTS(SELECT FROM photo.artifacts a WHERE a.id=d.artifact_id AND a.tenant_id=d.tenant_id) THEN RETURN;END IF;
 UPDATE photo.backend_documents SET state=CASE WHEN state='cleaned' THEN 'cleaned' ELSE 'cleaning' END,
 cleanup_token=fence,cleanup_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=d.id;
 RETURN QUERY SELECT d.id,fence,d.tenant_id,d.run_id,d.artifact_id,d.bucket;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_document_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_document_cleanup() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_document_cleanup() TO photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.finish_document_cleanup(p_document uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.backend_documents%ROWTYPE; d photo.backend_documents%ROWTYPE;
BEGIN
 SELECT * INTO hint FROM photo.backend_documents WHERE id=p_document;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM u.id FROM photo.tenant_usage u WHERE u.tenant_id=hint.tenant_id FOR UPDATE;
 PERFORM id FROM photo.runs WHERE id=hint.run_id AND tenant_id=hint.tenant_id FOR UPDATE;
 SELECT * INTO d FROM photo.backend_documents WHERE id=p_document FOR UPDATE;
 IF p_token IS NULL OR d.cleanup_token IS DISTINCT FROM p_token OR d.state NOT IN ('cleaning','cleaned')
 OR d.cleanup_lease_until IS NULL OR d.cleanup_lease_until<=clock_timestamp()
 OR EXISTS(SELECT FROM photo.artifacts a WHERE a.id=d.artifact_id AND a.tenant_id=d.tenant_id) THEN RETURN false;END IF;
 IF d.state='cleaning' THEN
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes WHERE tenant_id=d.tenant_id AND reserved_bytes>=d.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 END IF;
 UPDATE photo.backend_documents SET state='cleaned',cleanup_token=NULL,cleanup_lease_until=NULL,
 cleanup_after=clock_timestamp()+interval '24 hours' WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_document_cleanup(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_document_cleanup(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_document_cleanup(uuid,uuid) TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
