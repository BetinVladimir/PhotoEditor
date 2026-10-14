-- +goose Up
-- Older reservations may have an open Begin writer on the legacy archive key.
-- Only a privileged operator can settle it after proving the S3 Create call
-- has terminated. The runtime may then recover the bound ID or start a fresh
-- generation on a different key after an observed absence.
-- +goose StatementBegin
CREATE FUNCTION photo.reconcile_v30_archive_legacy_begin(
 p_tenant uuid,p_upload uuid,p_writer uuid,p_reservation uuid,
 p_action text,p_multipart text,p_writer_quiesced boolean,p_evidence_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;w photo.v30_archive_writers%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_upload IS NULL OR p_writer IS NULL OR
    p_reservation IS NULL OR p_action IS NULL OR
    p_action NOT IN ('bind','release') OR p_writer_quiesced IS DISTINCT FROM true OR
    p_evidence_sha IS NULL OR p_evidence_sha !~ '^[0-9a-f]{64}$'
 THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads
 WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.token IS DISTINCT FROM p_reservation OR
    d.state<>'reserved' OR d.multipart_id IS NOT NULL OR
    d.object_key IS DISTINCT FROM 'tenants/'||p_tenant||
      '/interchange-uploads/'||p_upload||'/archive.zip' OR
    EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g
      WHERE g.tenant_id=p_tenant AND g.upload_id=p_upload)
 THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v30_archive_writers
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer FOR UPDATE;
 IF NOT FOUND OR w.kind<>'begin' OR w.state<>'open' OR
    EXISTS(SELECT 1 FROM photo.v30_archive_writers other
      WHERE other.tenant_id=p_tenant AND other.upload_id=p_upload AND
      other.token<>p_writer AND other.state='open')
 THEN RETURN false;END IF;
 IF p_action='bind' THEN
   IF p_multipart IS NULL OR length(p_multipart) NOT BETWEEN 1 AND 1024 OR
      p_multipart ~ '[[:cntrl:]]' OR d.expires_at<=clock_timestamp() OR
      NOT EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active')
   THEN RETURN false;END IF;
   UPDATE photo.v30_archive_writers SET state='closed',receipt=p_multipart,
     closed_at=clock_timestamp() WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer;
   UPDATE photo.v30_archive_uploads SET state='uploading',multipart_id=p_multipart
   WHERE tenant_id=p_tenant AND id=p_upload;
 ELSE
   IF p_multipart IS NOT NULL THEN RETURN false;END IF;
   UPDATE photo.v30_archive_writers SET state='closed',
     receipt='operator-absent:'||p_evidence_sha,closed_at=clock_timestamp()
   WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer;
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reconcile_v30_archive_legacy_begin(uuid,uuid,uuid,uuid,text,text,boolean,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reconcile_v30_archive_legacy_begin(uuid,uuid,uuid,uuid,text,text,boolean,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,
      photo_maintenance,photo_publisher,photo_auth;

-- The legacy recovery reader must not misinterpret an operator absence marker
-- as an S3 multipart ID. The generation starter may pass a terminal, absent
-- legacy writer, but still refuses every unresolved or positively bound one.
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;
BEGIN
 SELECT pg_get_functiondef('photo.recover_v30_archive_begin(uuid,uuid,uuid,uuid,uuid)'::regprocedure) INTO definition;
 needle:='AND kind=''begin'' AND state=''closed'' ORDER BY';
 IF definition IS NULL OR position(needle IN definition)=0 OR
    position('operator-absent:' IN definition)>0 THEN
   RAISE EXCEPTION 'ARCHIVE_LEGACY_RECOVERY_SHAPE';END IF;
 EXECUTE replace(definition,needle,
   'AND kind=''begin'' AND state=''closed'' AND receipt !~ ''^operator-absent:[0-9a-f]{64}$'' ORDER BY');

 SELECT pg_get_functiondef('photo.start_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid)'::regprocedure) INTO definition;
 needle:=E'AND w.kind=''begin''\n   AND NOT EXISTS';
 IF definition IS NULL OR position(needle IN definition)=0 OR
    position('operator-absent:' IN definition)>0 THEN
   RAISE EXCEPTION 'ARCHIVE_GENERATION_START_SHAPE';END IF;
 EXECUTE replace(definition,needle,
   E'AND w.kind=''begin'' AND NOT (w.state=''closed'' AND w.receipt ~ ''^operator-absent:[0-9a-f]{64}$'')\n   AND NOT EXISTS');
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
