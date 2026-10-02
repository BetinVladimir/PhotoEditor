-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.accept_capture_receipt(p_tenant uuid,p_project uuid,
 p_device uuid,p_epoch bigint,p_session uuid,p_sequence bigint,p_sha text,
 p_upload uuid,p_captured timestamptz) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE existing photo.capture_receipts%ROWTYPE; u photo.upload_sessions%ROWTYPE;
 receipt photo.capture_receipts%ROWTYPE;
BEGIN
 IF p_session IS NULL OR p_upload IS NULL OR p_sequence IS NULL OR p_sequence<0
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'CAPTURE_RECEIPT_INVALID' USING ERRCODE='22023'; END IF;
 IF NOT photo.capture_device_admit_tx(p_tenant,p_project,p_device,p_epoch) THEN
  RAISE EXCEPTION 'CAPTURE_DEVICE_INVALID' USING ERRCODE='42501'; END IF;
 -- Serialize all sequences for one device, including requests with distinct
 -- HTTP idempotency keys. No owner DELETE privileges are used here.
 PERFORM pg_advisory_xact_lock(hashtextextended('capture-receipt/'||p_tenant||'/'||p_device,0));
 SELECT * INTO existing FROM photo.capture_receipts
  WHERE tenant_id=p_tenant AND device_id=p_device AND sequence=p_sequence;
 IF FOUND THEN
  IF existing.content_sha<>p_sha OR existing.session_id<>p_session THEN
   RAISE EXCEPTION 'CAPTURE_SEQUENCE_CONFLICT' USING ERRCODE='40001'; END IF;
  receipt:=existing;
 ELSE
  PERFORM 1 FROM photo.shoot_sessions WHERE tenant_id=p_tenant
   AND project_id=p_project AND id=p_session AND state<>'archived' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'CAPTURE_SESSION_MISSING' USING ERRCODE='P0002'; END IF;
  SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=p_tenant
   AND project_id=p_project AND id=p_upload FOR SHARE;
  IF NOT FOUND OR u.principal_type<>'device' OR u.principal_id<>p_device
   OR u.capture_session_id IS DISTINCT FROM p_session
   OR u.capture_credential_epoch IS DISTINCT FROM p_epoch THEN
   RAISE EXCEPTION 'CAPTURE_UPLOAD_MISSING' USING ERRCODE='P0002'; END IF;
  IF u.state<>'verified' OR u.verified_version_id IS NULL THEN
   RAISE EXCEPTION 'CAPTURE_UPLOAD_NOT_VERIFIED' USING ERRCODE='55000'; END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.ingest_upload_bindings b
   WHERE b.tenant_id=p_tenant AND b.upload_id=u.id AND b.run_id=u.ingest_run_id
    AND b.project_id=p_project AND b.version_id=u.verified_version_id
    AND b.source_sha=p_sha AND b.principal_type='device' AND b.principal_id=p_device)
  THEN RAISE EXCEPTION 'CAPTURE_CONTENT_MISMATCH' USING ERRCODE='22023'; END IF;
  INSERT INTO photo.capture_receipts(tenant_id,project_id,device_id,session_id,
   sequence,content_sha,upload_id,captured_at)
  VALUES(p_tenant,p_project,p_device,p_session,p_sequence,p_sha,p_upload,p_captured)
  RETURNING * INTO receipt;
  INSERT INTO photo.capture_receipt_assets(tenant_id,project_id,receipt_id,asset_version_id)
  VALUES(p_tenant,p_project,receipt.id,u.verified_version_id);
 END IF;
 RETURN jsonb_build_object('resource_type','capture_receipts','id',receipt.id,
  'tenant_id',receipt.tenant_id,'project_id',receipt.project_id,
  'created_at',receipt.created_at,'device_id',receipt.device_id,
  'session_id',receipt.session_id,'sequence',receipt.sequence::text,
  'content_sha',receipt.content_sha,'upload_id',receipt.upload_id,
  'captured_at',receipt.captured_at);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.accept_capture_receipt(uuid,uuid,uuid,bigint,uuid,bigint,text,uuid,timestamptz) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.accept_capture_receipt(uuid,uuid,uuid,bigint,uuid,bigint,text,uuid,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.accept_capture_receipt(uuid,uuid,uuid,bigint,uuid,bigint,text,uuid,timestamptz) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
