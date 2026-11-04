-- +goose Up
-- FN-D-21 (docs/video/20 section 5.4): retention of history. Batches of at most p_limit rows per rule, idempotent, returns the
-- number of rows removed or folded. Monthly partitions of video_egress_events are not created here: that needs ownership of
-- the partitioned table, which belongs to the migration owner, not to photo_fn_owner (see docs/video/PLAN.md section 8).

-- +goose StatementBegin
CREATE FUNCTION photo.purge_video_history(p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n integer:=0;k integer;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 1000 THEN RAISE EXCEPTION 'LIMIT_INVALID' USING ERRCODE='22023';END IF;
 -- upload sessions that never became media, 90 days after their last change (parts go with them)
 WITH gone AS (SELECT tenant_id,id FROM photo.video_upload_sessions s
   WHERE s.state IN('rejected','aborted','expired') AND s.updated_at<clock_timestamp()-interval '90 days'
    AND NOT EXISTS(SELECT FROM photo.video_media m WHERE m.tenant_id=s.tenant_id AND m.upload_id=s.id)
   ORDER BY s.updated_at LIMIT p_limit FOR UPDATE SKIP LOCKED),
 parts AS (DELETE FROM photo.video_upload_parts p USING gone WHERE p.tenant_id=gone.tenant_id AND p.upload_id=gone.id)
 DELETE FROM photo.video_upload_sessions s USING gone WHERE s.tenant_id=gone.tenant_id AND s.id=gone.id;
 GET DIAGNOSTICS k=ROW_COUNT;n:=n+k;
 -- terminal jobs nobody points at, 180 days after they finished
 WITH gone AS (SELECT tenant_id,id FROM photo.video_jobs j
   WHERE j.state IN('succeeded','failed','cancelled','expired') AND coalesce(j.finished_at,j.created_at)<clock_timestamp()-interval '180 days'
    AND NOT EXISTS(SELECT FROM photo.video_renders r WHERE r.tenant_id=j.tenant_id AND r.job_id=j.id)
    AND NOT EXISTS(SELECT FROM photo.video_media m WHERE m.tenant_id=j.tenant_id AND m.ingest_job_id=j.id)
   ORDER BY coalesce(j.finished_at,j.created_at) LIMIT p_limit FOR UPDATE SKIP LOCKED),
 outs AS (DELETE FROM photo.video_job_outputs o USING gone WHERE o.tenant_id=gone.tenant_id AND o.job_id=gone.id)
 DELETE FROM photo.video_jobs j USING gone WHERE j.tenant_id=gone.tenant_id AND j.id=gone.id;
 GET DIAGNOSTICS k=ROW_COUNT;n:=n+k;
 -- hourly stream counters older than 400 days are folded into one row per day
 WITH old AS (DELETE FROM photo.video_egress_counters c
   WHERE ctid IN(SELECT ctid FROM photo.video_egress_counters WHERE bucket_start<date_trunc('day',clock_timestamp()-interval '400 days')
     AND bucket_start<>date_trunc('day',bucket_start) ORDER BY bucket_start LIMIT p_limit)
   RETURNING tenant_id,share_id,render_id,kind,bucket_start,bytes,requests),
 folded AS (SELECT tenant_id,share_id,render_id,kind,date_trunc('day',bucket_start) AS day,sum(bytes) AS bytes,sum(requests) AS requests FROM old GROUP BY 1,2,3,4,5)
 INSERT INTO photo.video_egress_counters(tenant_id,share_id,render_id,kind,bucket_start,bytes,requests)
  SELECT tenant_id,share_id,render_id,kind,day,bytes,requests FROM folded
  ON CONFLICT(tenant_id,share_id,render_id,kind,bucket_start) DO UPDATE SET bytes=video_egress_counters.bytes+EXCLUDED.bytes,requests=video_egress_counters.requests+EXCLUDED.requests;
 GET DIAGNOSTICS k=ROW_COUNT;n:=n+k;
 RETURN n;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.purge_video_history(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.purge_video_history(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.purge_video_history(integer) TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
