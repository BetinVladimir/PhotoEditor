-- +goose Up
-- FN-D-16: fold old autosave revisions. FN-D-22: reconcile media left in 'probing' without a live ingest job.

-- Autosave revisions older than 30 days are deleted except the newest one of each UTC day; the newest revision of a sequence,
-- manual/render/cut revisions and anything a render or job points at always stay. The document object is released for the
-- artifact cleaner (expires_at), exactly as a rejected source is.
-- +goose StatementBegin
CREATE FUNCTION photo.compact_video_autosaves(p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r record;n integer:=0;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 1000 THEN RAISE EXCEPTION 'LIMIT_INVALID' USING ERRCODE='22023';END IF;
 PERFORM set_config('photo.video_gc','on',true);
 FOR r IN SELECT v.tenant_id,v.sequence_id,v.revision,v.document_artifact_id FROM photo.video_sequence_revisions v
  WHERE v.label='autosave' AND v.created_at<clock_timestamp()-interval '30 days'
   AND v.revision<(SELECT max(x.revision) FROM photo.video_sequence_revisions x WHERE x.tenant_id=v.tenant_id AND x.sequence_id=v.sequence_id)
   AND EXISTS(SELECT FROM photo.video_sequence_revisions y WHERE y.tenant_id=v.tenant_id AND y.sequence_id=v.sequence_id AND y.label='autosave'
    AND y.revision>v.revision AND (y.created_at AT TIME ZONE 'UTC')::date=(v.created_at AT TIME ZONE 'UTC')::date)
   AND NOT EXISTS(SELECT FROM photo.video_renders d WHERE d.tenant_id=v.tenant_id AND d.sequence_id=v.sequence_id AND d.sequence_revision=v.revision)
   AND NOT EXISTS(SELECT FROM photo.video_jobs j WHERE j.tenant_id=v.tenant_id AND j.sequence_id=v.sequence_id AND j.sequence_revision=v.revision)
  ORDER BY v.created_at LIMIT p_limit FOR UPDATE OF v SKIP LOCKED LOOP
  DELETE FROM photo.video_sequence_sources WHERE tenant_id=r.tenant_id AND sequence_id=r.sequence_id AND revision=r.revision;
  DELETE FROM photo.video_sequence_revisions WHERE tenant_id=r.tenant_id AND sequence_id=r.sequence_id AND revision=r.revision;
  UPDATE photo.artifacts SET expires_at=clock_timestamp() WHERE tenant_id=r.tenant_id AND id=r.document_artifact_id
   AND NOT EXISTS(SELECT FROM photo.video_sequence_revisions z WHERE z.tenant_id=r.tenant_id AND z.document_artifact_id=r.document_artifact_id);
  n:=n+1;
 END LOOP;
 RETURN n;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.reconcile_video_stuck(p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE m record;n integer:=0;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 1000 THEN RAISE EXCEPTION 'LIMIT_INVALID' USING ERRCODE='22023';END IF;
 FOR m IN SELECT x.tenant_id,x.id FROM photo.video_media x
  WHERE x.state='probing' AND x.updated_at<clock_timestamp()-interval '15 minutes'
   AND NOT EXISTS(SELECT FROM photo.video_jobs j WHERE j.tenant_id=x.tenant_id AND j.media_id=x.id AND j.kind='ingest' AND j.state IN('queued','running'))
  ORDER BY x.updated_at LIMIT p_limit FOR UPDATE OF x SKIP LOCKED LOOP
  IF photo.finish_video_ingest(m.tenant_id,m.id,'rejected','VIDEO_INGEST_ABANDONED',NULL,NULL,NULL,NULL,NULL) THEN n:=n+1;END IF;
 END LOOP;
 RETURN n;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.compact_video_autosaves(integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.reconcile_video_stuck(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.compact_video_autosaves(integer),photo.reconcile_video_stuck(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.compact_video_autosaves(integer),photo.reconcile_video_stuck(integer) TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
