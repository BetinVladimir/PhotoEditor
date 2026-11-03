-- +goose Up
-- FN-D-06 finish_video_ingest and FN-D-07 save_video_sequence_revision.

-- Probe/derivative results arrive as already registered ready artifacts. 'ready' moves the reservation to
-- storage (source + derivatives must fit the reservation, VID-Q-05), 'rejected' releases it. Replays are no-ops.
-- +goose StatementBegin
CREATE FUNCTION photo.finish_video_ingest(p_tenant uuid,p_media uuid,p_state text,p_reject_code text,p_probe jsonb,
 p_probe_artifact uuid,p_proxy uuid,p_filmstrip uuid,p_waveform uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE m photo.video_media%ROWTYPE;s photo.video_upload_sessions%ROWTYPE;r photo.quota_reservations%ROWTYPE;
 derivative_bytes bigint:=0;a record;want record;total bigint;
BEGIN
 IF p_state NOT IN('ready','rejected') THEN RAISE EXCEPTION 'VIDEO_INGEST_STATE' USING ERRCODE='22023';END IF;
 SELECT * INTO m FROM photo.video_media WHERE tenant_id=p_tenant AND id=p_media FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF m.state<>'probing' THEN RETURN m.state=p_state;END IF;
 SELECT * INTO s FROM photo.video_upload_sessions WHERE tenant_id=p_tenant AND id=m.upload_id FOR UPDATE;
 SELECT * INTO r FROM photo.quota_reservations WHERE tenant_id=p_tenant AND id=s.reservation_id AND state='reserved' AND holder_type='video_upload' AND holder_id=s.id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'VIDEO_RESERVATION_MISSING' USING ERRCODE='55000';END IF;
 IF p_state='rejected' THEN
  IF p_reject_code IS NULL OR length(p_reject_code) NOT BETWEEN 1 AND 64 THEN RAISE EXCEPTION 'VIDEO_INGEST_REJECT_CODE' USING ERRCODE='22023';END IF;
  UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=p_tenant AND id=r.id;
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-r.bytes WHERE tenant_id=p_tenant;
  UPDATE photo.artifacts SET expires_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=m.source_artifact_id;
  UPDATE photo.video_media SET state='rejected',reject_code=p_reject_code,revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=m.id;
  UPDATE photo.video_upload_sessions SET state='rejected',reject_code=p_reject_code,revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=s.id;
  RETURN true;END IF;
 IF p_probe IS NULL OR jsonb_typeof(p_probe)<>'object' OR p_probe_artifact IS NULL THEN RAISE EXCEPTION 'VIDEO_INGEST_PROBE' USING ERRCODE='22023';END IF;
 FOR want IN SELECT * FROM (VALUES(p_probe_artifact,'video_probe'),(p_proxy,'video_proxy'),(p_filmstrip,'video_filmstrip'),(p_waveform,'video_waveform')) v(id,role) WHERE id IS NOT NULL LOOP
  SELECT bytes INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=want.id AND state='ready' AND output_role=want.role;
  IF NOT FOUND THEN RAISE EXCEPTION 'VIDEO_DERIVATIVE_MISSING: %',want.role USING ERRCODE='55000';END IF;
  derivative_bytes:=derivative_bytes+a.bytes;
 END LOOP;
 total:=m.source_bytes+derivative_bytes;
 IF total>r.bytes THEN RAISE EXCEPTION 'QUOTA_EXCEEDED' USING ERRCODE='53400';END IF;
 UPDATE photo.video_media SET state='ready',probe=p_probe,probe_artifact_id=p_probe_artifact,proxy_artifact_id=p_proxy,filmstrip_artifact_id=p_filmstrip,
  waveform_artifact_id=p_waveform,revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=m.id;
 UPDATE photo.quota_reservations SET state='consumed' WHERE tenant_id=p_tenant AND id=r.id;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-r.bytes,storage_bytes=storage_bytes+total WHERE tenant_id=p_tenant;
 UPDATE photo.video_upload_sessions SET state='verified',revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=s.id;
 RETURN true;
END $$;
-- +goose StatementEnd

-- Append a sequence revision (CAS on head_revision). Sources: [{"kind","id","sha256"}].
-- +goose StatementBegin
CREATE FUNCTION photo.save_video_sequence_revision(p_tenant uuid,p_sequence uuid,p_expected bigint,p_document uuid,p_sha text,
 p_summary jsonb,p_sources jsonb,p_label text,p_user uuid) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s photo.video_sequences%ROWTYPE;rev bigint;src record;head photo.video_sequence_revisions%ROWTYPE;ok boolean;
BEGIN
 IF p_expected IS NULL OR p_expected<0 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_summary IS NULL OR jsonb_typeof(p_summary)<>'object'
  OR p_sources IS NULL OR jsonb_typeof(p_sources)<>'array' OR jsonb_array_length(p_sources)>256 THEN RAISE EXCEPTION 'VIDEO_SEQUENCE_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO s FROM photo.video_sequences WHERE tenant_id=p_tenant AND id=p_sequence FOR UPDATE;
 IF NOT FOUND OR s.archived_at IS NOT NULL THEN RAISE EXCEPTION 'VIDEO_SEQUENCE_NOT_FOUND' USING ERRCODE='P0002';END IF;
 IF s.head_revision=p_expected+1 THEN
  SELECT * INTO head FROM photo.video_sequence_revisions WHERE tenant_id=p_tenant AND sequence_id=p_sequence AND revision=s.head_revision;
  IF head.document_sha256=p_sha AND head.label=p_label AND head.created_by=p_user THEN RETURN head.revision;END IF;END IF;
 IF s.head_revision<>p_expected THEN
  RAISE EXCEPTION 'VIDEO_SEQUENCE_REVISION_CONFLICT' USING ERRCODE='40001',DETAIL=format('head_revision=%s',s.head_revision);END IF;
 SELECT true INTO ok FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_document AND state='ready' AND sha256=p_sha AND output_role='video_sequence_document';
 IF ok IS NOT TRUE THEN RAISE EXCEPTION 'VIDEO_DOCUMENT_MISMATCH' USING ERRCODE='22023';END IF;
 rev:=p_expected+1;
 INSERT INTO photo.video_sequence_revisions(tenant_id,sequence_id,revision,parent_revision,document_artifact_id,document_sha256,summary,label,created_by)
  VALUES(p_tenant,p_sequence,rev,CASE WHEN p_expected=0 THEN NULL ELSE p_expected END,p_document,p_sha,p_summary,p_label,p_user);
 FOR src IN SELECT * FROM jsonb_to_recordset(p_sources) AS x(kind text,id uuid,sha256 text) LOOP
  IF src.kind IN('video','audio','lut') THEN
   IF NOT EXISTS(SELECT FROM photo.video_media m WHERE m.tenant_id=p_tenant AND m.project_id=s.project_id AND m.id=src.id AND m.kind=src.kind AND m.state='ready' AND m.source_sha256=src.sha256) THEN
    RAISE EXCEPTION 'VIDEO_SOURCE_NOT_READY: %',src.id USING ERRCODE='22023';END IF;
  ELSIF src.kind='photo' THEN
   IF NOT EXISTS(SELECT FROM photo.assets a WHERE a.tenant_id=p_tenant AND a.project_id=s.project_id AND a.id=src.id) THEN
    RAISE EXCEPTION 'VIDEO_SOURCE_NOT_READY: %',src.id USING ERRCODE='22023';END IF;
  ELSE RAISE EXCEPTION 'VIDEO_SEQUENCE_INVALID' USING ERRCODE='22023';END IF;
  INSERT INTO photo.video_sequence_sources(tenant_id,sequence_id,revision,source_kind,source_id,source_sha256) VALUES(p_tenant,p_sequence,rev,src.kind,src.id,src.sha256)
   ON CONFLICT DO NOTHING;
 END LOOP;
 UPDATE photo.video_sequences SET head_revision=rev,updated_at=clock_timestamp(),revision=revision+1 WHERE tenant_id=p_tenant AND id=p_sequence;
 RETURN rev;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.finish_video_ingest(uuid,uuid,text,text,jsonb,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.save_video_sequence_revision(uuid,uuid,bigint,uuid,text,jsonb,jsonb,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_video_ingest(uuid,uuid,text,text,jsonb,uuid,uuid,uuid,uuid),
 photo.save_video_sequence_revision(uuid,uuid,bigint,uuid,text,jsonb,jsonb,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_video_ingest(uuid,uuid,text,text,jsonb,uuid,uuid,uuid,uuid) TO photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.save_video_sequence_revision(uuid,uuid,bigint,uuid,text,jsonb,jsonb,text,uuid) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
