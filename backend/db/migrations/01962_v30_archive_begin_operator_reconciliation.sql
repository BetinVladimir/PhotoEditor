-- +goose Up
-- A privileged operator may settle only the exact open generation after
-- independently proving its non-idempotent S3 Create request is terminal.
-- This function is deliberately unavailable to every runtime role.
ALTER TABLE photo.v30_archive_begin_generations
 ADD COLUMN operator_reconciliation_evidence_sha text;
ALTER TABLE photo.v30_archive_begin_generations
 ADD CONSTRAINT operator_reconciliation_evidence_shape
 CHECK (operator_reconciliation_evidence_sha IS NULL OR
        operator_reconciliation_evidence_sha ~ '^[0-9a-f]{64}$');
-- +goose StatementBegin
CREATE FUNCTION photo.reconcile_v30_archive_begin_generation(
 p_tenant uuid,p_upload uuid,p_generation uuid,p_writer uuid,p_reservation uuid,
 p_action text,p_multipart text,p_writer_quiesced boolean,p_evidence_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
        g photo.v30_archive_begin_generations%ROWTYPE;
        w photo.v30_archive_writers%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_upload IS NULL OR p_generation IS NULL OR
    p_writer IS NULL OR p_reservation IS NULL OR
    p_action IS NULL OR p_action NOT IN ('bind','release') OR
    p_writer_quiesced IS DISTINCT FROM true OR p_evidence_sha IS NULL OR
    p_evidence_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads
 WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.token IS DISTINCT FROM p_reservation OR d.state<>'reserved' OR
    d.multipart_id IS NOT NULL OR d.request_sha IS NULL THEN RETURN false;END IF;
 SELECT * INTO g FROM photo.v30_archive_begin_generations
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation FOR UPDATE;
 IF NOT FOUND OR g.state<>'creating' OR g.multipart_id IS NOT NULL OR
    g.writer_token IS DISTINCT FROM p_writer OR
    g.reservation_token IS DISTINCT FROM p_reservation OR
    g.request_sha IS DISTINCT FROM d.request_sha OR
    g.creator_user_id IS DISTINCT FROM d.actor_id OR
    g.object_key IS DISTINCT FROM 'tenants/'||p_tenant||
      '/interchange-uploads/'||p_upload||'/generations/'||p_generation||'/archive.zip'
 THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v30_archive_writers
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer FOR UPDATE;
 IF NOT FOUND OR w.kind<>'begin' OR w.state<>'open' THEN RETURN false;END IF;
 IF p_action='bind' THEN
   IF p_multipart IS NULL OR length(p_multipart) NOT BETWEEN 1 AND 1024 OR
      p_multipart ~ '[[:cntrl:]]' OR d.expires_at<=clock_timestamp() OR
      NOT EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active')
   THEN RETURN false;END IF;
   UPDATE photo.v30_archive_writers SET state='closed',receipt=p_multipart,
     closed_at=clock_timestamp() WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer;
   UPDATE photo.v30_archive_begin_generations SET state='bound',multipart_id=p_multipart,
     terminal_evidence='s3-create-receipt',terminal_at=clock_timestamp(),
     operator_reconciliation_evidence_sha=p_evidence_sha,revision=revision+1
   WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation;
   UPDATE photo.v30_archive_uploads SET state='uploading',multipart_id=p_multipart,
     object_key=g.object_key WHERE tenant_id=p_tenant AND id=p_upload;
 ELSE
   -- Release requires a terminal Create plus exact-key proof of absence.
   -- The new attempt will use a different generation/key after this commits.
   IF p_multipart IS NOT NULL THEN RETURN false;END IF;
   UPDATE photo.v30_archive_writers SET state='closed',
     receipt='operator-absent:'||p_evidence_sha,closed_at=clock_timestamp()
   WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer;
   UPDATE photo.v30_archive_begin_generations SET state='aborted',
     terminal_evidence='operator-absent:'||p_evidence_sha,
     terminal_at=clock_timestamp(),purged_at=clock_timestamp(),
     operator_reconciliation_evidence_sha=p_evidence_sha,revision=revision+1
   WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation;
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reconcile_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,text,text,boolean,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reconcile_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,text,text,boolean,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,
      photo_maintenance,photo_publisher,photo_auth;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
