-- +goose Up
ALTER TABLE photo.upload_sessions
 ADD COLUMN device_begin_reconciled_at timestamptz,
 ADD COLUMN device_begin_reconcile_evidence_sha text,
 ADD COLUMN device_begin_reconcile_count bigint NOT NULL DEFAULT 0;
ALTER TABLE photo.upload_sessions ADD CONSTRAINT device_begin_reconcile_evidence_shape
 CHECK (device_begin_reconcile_evidence_sha IS NULL OR
        device_begin_reconcile_evidence_sha ~ '^[0-9a-f]{64}$');
ALTER TABLE photo.upload_sessions ADD CONSTRAINT device_begin_reconcile_count_shape
 CHECK (device_begin_reconcile_count >= 0);

-- Only the function owner / privileged database operator can invoke this.
-- Runtime roles must never turn a storage listing into writer-quiescence proof.
-- +goose StatementBegin
CREATE FUNCTION photo.reconcile_device_begin(
 p_tenant uuid,p_upload uuid,p_claim uuid,p_action text,p_multipart text,
 p_writer_quiesced boolean,p_evidence_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.upload_sessions%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_upload IS NULL OR p_claim IS NULL OR
    p_writer_quiesced IS DISTINCT FROM true OR
    p_evidence_sha !~ '^[0-9a-f]{64}$' OR p_evidence_sha IS NULL OR
    p_action IS NULL OR p_action NOT IN ('bind','release') THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.upload_sessions
 WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.principal_type<>'device' OR d.state<>'initializing' OR
    d.multipart_id IS NOT NULL OR d.device_begin_token IS DISTINCT FROM p_claim OR
    d.object_key IS DISTINCT FROM 'staging/tenants/'||p_tenant||'/uploads/'||p_upload OR
    d.expires_at<=clock_timestamp()
 THEN RETURN false;END IF;
 IF p_action='bind' THEN
   IF p_multipart IS NULL OR length(p_multipart) NOT BETWEEN 1 AND 1024 OR
      p_multipart ~ '[[:cntrl:]]' THEN RETURN false;END IF;
   UPDATE photo.upload_sessions SET multipart_id=p_multipart,state='uploading',
     device_begin_reconciled_at=clock_timestamp(),
     device_begin_reconcile_evidence_sha=p_evidence_sha,
     device_begin_reconcile_count=device_begin_reconcile_count+1,revision=revision+1
   WHERE tenant_id=p_tenant AND id=p_upload;
 ELSE
   -- Release is permitted only after the operator proved that the original
   -- writer terminated and no remote multipart exists for this exact key.
   IF p_multipart IS NOT NULL THEN RETURN false;END IF;
   UPDATE photo.upload_sessions SET device_begin_token=NULL,
     device_begin_reconciled_at=clock_timestamp(),
     device_begin_reconcile_evidence_sha=p_evidence_sha,
     device_begin_reconcile_count=device_begin_reconcile_count+1,revision=revision+1
   WHERE tenant_id=p_tenant AND id=p_upload;
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reconcile_device_begin(uuid,uuid,uuid,text,text,boolean,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reconcile_device_begin(uuid,uuid,uuid,text,text,boolean,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,
      photo_maintenance,photo_publisher,photo_auth;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
