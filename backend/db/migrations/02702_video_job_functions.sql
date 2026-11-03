-- +goose Up
-- Video job queue (FN-D-08..11, FN-D-17 and helpers). Jobs wait in photo.video_jobs and are claimed by video workers
-- with a lease + fence token (claim_video_job); photo runs/tasks are not used. Cost and admission follow
-- docs/video/20-saas-scaling-operations.md 3.1-3.4 (constants of DS-01).

-- FN-Q-09
-- +goose StatementBegin
CREATE FUNCTION photo.video_job_cost_seconds(p_kind text,p_ticks bigint) RETURNS integer
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s bigint;
BEGIN
 IF p_kind IS NULL OR p_kind NOT IN('ingest','detect_cuts','color_analyze','audio_analyze','preview_frames','render') THEN
  RAISE EXCEPTION 'KIND_INVALID' USING ERRCODE='22023';END IF;
 IF p_ticks IS NULL OR p_ticks<0 OR (p_kind<>'preview_frames' AND p_ticks=0) THEN RAISE EXCEPTION 'DURATION_INVALID' USING ERRCODE='22023';END IF;
 s:=(p_ticks+705600000-1)/705600000;
 RETURN CASE p_kind WHEN 'ingest' THEN 30+(s*3+4)/5 WHEN 'detect_cuts' THEN 60+(s*7+19)/20 WHEN 'color_analyze' THEN 30+(s+4)/5
  WHEN 'audio_analyze' THEN 20+(s+19)/20 WHEN 'preview_frames' THEN 5 ELSE 60+(s*3+1)/2 END;
END $$;
-- +goose StatementEnd

-- FN-Q-10
-- +goose StatementBegin
CREATE FUNCTION photo.video_retry_after_seconds(p_ahead bigint,p_workers integer) RETURNS integer
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE raw bigint;
BEGIN
 IF p_ahead IS NULL OR p_ahead<0 THEN RAISE EXCEPTION 'AHEAD_INVALID' USING ERRCODE='22023';END IF;
 raw:=least(900,greatest(5,(p_ahead+greatest(1,coalesce(p_workers,1))-1)/greatest(1,coalesce(p_workers,1))));
 RETURN ((raw+4)/5)*5;
END $$;
-- +goose StatementEnd

-- FN-D-17: steps 2-4 of 3.3. Raises VIDEO_TENANT_LIMIT (DETAIL = limit code) or VIDEO_CAPACITY_BUSY (DETAIL = retry_after_s).
-- Changes nothing; the daily counter is bumped by create_video_job on success only.
-- +goose StatementBegin
CREATE FUNCTION photo.admit_video_job(p_tenant uuid,p_kind text,p_cost integer,p_workers integer,p_shed_cost bigint DEFAULT 21600) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE lim photo.video_tenant_limits%ROWTYPE;grp text;pool_name text;cap integer;used integer;daily bigint;used_cost bigint;ahead bigint;
BEGIN
 grp:=CASE p_kind WHEN 'ingest' THEN 'ingest' WHEN 'render' THEN 'heavy' WHEN 'detect_cuts' THEN 'heavy' WHEN 'color_analyze' THEN 'heavy'
  WHEN 'audio_analyze' THEN 'light' WHEN 'preview_frames' THEN 'preview' END;
 IF grp IS NULL THEN RAISE EXCEPTION 'KIND_INVALID' USING ERRCODE='22023';END IF;
 pool_name:=CASE WHEN grp IN('light','preview') THEN 'video_light' ELSE 'video_heavy' END;
 SELECT * INTO lim FROM photo.video_tenant_limits WHERE tenant_id=p_tenant;
 cap:=CASE grp WHEN 'ingest' THEN coalesce(lim.max_outstanding_ingest,50) WHEN 'heavy' THEN coalesce(lim.max_outstanding_heavy,4)
  WHEN 'light' THEN coalesce(lim.max_outstanding_light,8) ELSE coalesce(lim.max_outstanding_preview,4) END;
 SELECT count(*) INTO used FROM photo.video_jobs j WHERE j.tenant_id=p_tenant AND j.state IN('queued','running')
  AND CASE j.kind WHEN 'ingest' THEN 'ingest' WHEN 'audio_analyze' THEN 'light' WHEN 'preview_frames' THEN 'preview' ELSE 'heavy' END=grp;
 IF used+1>cap THEN RAISE EXCEPTION 'VIDEO_TENANT_LIMIT' USING ERRCODE='53400',DETAIL='OUTSTANDING_'||upper(grp);END IF;
 daily:=CASE WHEN lim.daily_cost_unlimited THEN NULL ELSE coalesce(lim.daily_cost_seconds,14400) END;
 IF daily IS NOT NULL THEN
  SELECT coalesce(cost_seconds,0) INTO used_cost FROM photo.video_tenant_usage_daily WHERE tenant_id=p_tenant AND day=(clock_timestamp() AT TIME ZONE 'UTC')::date;
  IF coalesce(used_cost,0)+p_cost>daily THEN RAISE EXCEPTION 'VIDEO_TENANT_LIMIT' USING ERRCODE='53400',DETAIL='DAILY_COST';END IF;END IF;
 IF grp<>'preview' THEN
  SELECT coalesce(sum(cost_seconds),0) INTO ahead FROM photo.video_jobs WHERE state='queued' AND pool=pool_name;
  IF ahead>p_shed_cost THEN RAISE EXCEPTION 'VIDEO_CAPACITY_BUSY' USING ERRCODE='53300',DETAIL=photo.video_retry_after_seconds(ahead,p_workers)::text;END IF;END IF;
END $$;
-- +goose StatementEnd

-- FN-D-08. p_reserve_bytes=0 creates no reservation (ingest uses the upload reservation).
-- +goose StatementBegin
CREATE FUNCTION photo.create_video_job(p_tenant uuid,p_project uuid,p_user uuid,p_kind text,p_media uuid,p_sequence uuid,p_revision bigint,
 p_params_artifact uuid,p_params_sha text,p_backend text,p_duration_ticks bigint,p_request_sha text,p_reserve_bytes bigint,p_workers integer)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.video_jobs%ROWTYPE;cost integer;pool_name text;jid uuid:=gen_random_uuid();rid uuid;used bigint;reserved bigint;quota bigint;
BEGIN
 SELECT * INTO j FROM photo.video_jobs WHERE tenant_id=p_tenant AND created_by=p_user AND request_sha256=p_request_sha;
 IF FOUND THEN
  IF j.kind<>p_kind OR j.params_sha256<>p_params_sha OR j.media_id IS DISTINCT FROM p_media OR j.sequence_id IS DISTINCT FROM p_sequence THEN
   RAISE EXCEPTION 'IDEMPOTENCY_KEY_REUSED' USING ERRCODE='23505';END IF;
  RETURN j.id;END IF;
 IF p_reserve_bytes IS NULL OR p_reserve_bytes<0 OR p_backend NOT IN('auto','cuda','mlx','cpu') THEN RAISE EXCEPTION 'VIDEO_JOB_INVALID' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=p_tenant AND p.id=p_project AND p.archived_at IS NULL) THEN RAISE EXCEPTION 'PROJECT_NOT_FOUND' USING ERRCODE='P0002';END IF;
 IF p_kind='ingest' THEN
  IF NOT EXISTS(SELECT FROM photo.video_media m WHERE m.tenant_id=p_tenant AND m.project_id=p_project AND m.id=p_media AND m.state='probing') THEN RAISE EXCEPTION 'VIDEO_MEDIA_NOT_FOUND' USING ERRCODE='P0002';END IF;
 ELSIF p_kind IN('detect_cuts','color_analyze','audio_analyze') THEN
  IF NOT EXISTS(SELECT FROM photo.video_media m WHERE m.tenant_id=p_tenant AND m.project_id=p_project AND m.id=p_media AND m.state='ready') THEN RAISE EXCEPTION 'VIDEO_MEDIA_NOT_FOUND' USING ERRCODE='P0002';END IF;
 ELSIF p_kind IN('preview_frames','render') THEN
  IF NOT EXISTS(SELECT FROM photo.video_sequence_revisions r JOIN photo.video_sequences s ON s.tenant_id=r.tenant_id AND s.id=r.sequence_id
   WHERE r.tenant_id=p_tenant AND s.project_id=p_project AND r.sequence_id=p_sequence AND r.revision=p_revision) THEN RAISE EXCEPTION 'VIDEO_SEQUENCE_NOT_FOUND' USING ERRCODE='P0002';END IF;
 END IF;
 cost:=photo.video_job_cost_seconds(p_kind,p_duration_ticks);
 pool_name:=CASE WHEN p_kind IN('audio_analyze','preview_frames') THEN 'video_light' ELSE 'video_heavy' END;
 PERFORM 1 FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM photo.admit_video_job(p_tenant,p_kind,cost,p_workers);
 IF p_reserve_bytes>0 THEN
  SELECT u.storage_bytes,u.reserved_bytes,(t.quotas->>'storage_bytes')::bigint INTO used,reserved,quota
   FROM photo.tenant_usage u JOIN photo.tenants t ON t.id=u.tenant_id WHERE u.tenant_id=p_tenant AND t.status='active';
  IF quota IS NULL OR quota<p_reserve_bytes OR used>quota-p_reserve_bytes OR reserved>quota-p_reserve_bytes-used THEN RAISE EXCEPTION 'QUOTA_EXCEEDED' USING ERRCODE='53400';END IF;
  rid:=gen_random_uuid();
  INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at)
   VALUES(rid,p_tenant,'user',p_user,'video_job',jid,p_reserve_bytes,0,'reserved',clock_timestamp()+interval '7 days');
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_reserve_bytes WHERE tenant_id=p_tenant;END IF;
 INSERT INTO photo.video_jobs(id,tenant_id,project_id,kind,media_id,sequence_id,sequence_revision,params_artifact_id,params_sha256,state,backend_requested,
  reservation_id,cost_seconds,pool,created_by,request_sha256)
  VALUES(jid,p_tenant,p_project,p_kind,p_media,p_sequence,p_revision,p_params_artifact,p_params_sha,'queued',p_backend,rid,cost,pool_name,p_user,p_request_sha);
 INSERT INTO photo.video_tenant_usage_daily(tenant_id,day,cost_seconds,jobs_admitted) VALUES(p_tenant,(clock_timestamp() AT TIME ZONE 'UTC')::date,cost,1)
  ON CONFLICT(tenant_id,day) DO UPDATE SET cost_seconds=photo.video_tenant_usage_daily.cost_seconds+cost,jobs_admitted=photo.video_tenant_usage_daily.jobs_admitted+1;
 RETURN jid;
END $$;
-- +goose StatementEnd

-- FN-D-11: render = render job + video_renders row; the frozen document hash comes from the sequence revision.
-- +goose StatementBegin
CREATE FUNCTION photo.create_video_render(p_tenant uuid,p_project uuid,p_user uuid,p_sequence uuid,p_revision bigint,p_preset jsonb,p_preset_sha text,
 p_params_artifact uuid,p_params_sha text,p_backend text,p_duration_ticks bigint,p_request_sha text,p_reserve_bytes bigint,p_workers integer)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE jid uuid;rid uuid;s photo.video_sequences%ROWTYPE;doc_sha text;w integer;h integer;
BEGIN
 IF p_reserve_bytes IS NULL OR p_reserve_bytes<=0 OR p_preset IS NULL OR jsonb_typeof(p_preset)<>'object' THEN RAISE EXCEPTION 'VIDEO_JOB_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO s FROM photo.video_sequences WHERE tenant_id=p_tenant AND project_id=p_project AND id=p_sequence AND archived_at IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION 'VIDEO_SEQUENCE_NOT_FOUND' USING ERRCODE='P0002';END IF;
 SELECT document_sha256 INTO doc_sha FROM photo.video_sequence_revisions WHERE tenant_id=p_tenant AND sequence_id=p_sequence AND revision=p_revision;
 IF NOT FOUND THEN RAISE EXCEPTION 'VIDEO_SEQUENCE_NOT_FOUND' USING ERRCODE='P0002';END IF;
 jid:=photo.create_video_job(p_tenant,p_project,p_user,'render',NULL,p_sequence,p_revision,p_params_artifact,p_params_sha,p_backend,p_duration_ticks,p_request_sha,p_reserve_bytes,p_workers);
 SELECT id INTO rid FROM photo.video_renders WHERE tenant_id=p_tenant AND job_id=jid;
 IF FOUND THEN RETURN rid;END IF;
 w:=coalesce((p_preset->>'width')::integer,s.width);h:=coalesce((p_preset->>'height')::integer,s.height);
 INSERT INTO photo.video_renders(tenant_id,project_id,sequence_id,sequence_revision,document_sha256,preset,preset_sha256,job_id,width,height,fps_num,fps_den,
  video_codec,audio_codec,state,created_by)
  VALUES(p_tenant,p_project,p_sequence,p_revision,doc_sha,p_preset,p_preset_sha,jid,w,h,s.fps_num,s.fps_den,'h264','aac','queued',p_user) RETURNING id INTO rid;
 RETURN rid;
END $$;
-- +goose StatementEnd

-- Worker side: oldest queued job of the pool (tenant rotation by oldest-first per tenant), or a job whose lease expired.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_video_job(p_pool text,p_lease_seconds integer)
RETURNS TABLE(job_id uuid,tenant_id uuid,project_id uuid,kind text,media_id uuid,sequence_id uuid,sequence_revision bigint,params_artifact_id uuid,
 params_sha256 text,backend_requested text,claim_token uuid,attempt integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_column
DECLARE j photo.video_jobs%ROWTYPE;tok uuid:=gen_random_uuid();
BEGIN
 IF p_pool NOT IN('video_light','video_heavy') OR p_lease_seconds IS NULL OR p_lease_seconds NOT BETWEEN 10 AND 600 THEN RAISE EXCEPTION 'VIDEO_CLAIM_INVALID' USING ERRCODE='22023';END IF;
 -- jobs whose worker vanished: requeue (attempt<3) or abandon
 FOR j IN SELECT * FROM photo.video_jobs v WHERE v.pool=p_pool AND v.state='running' AND v.lease_until<=clock_timestamp() ORDER BY v.lease_until LIMIT 20 FOR UPDATE SKIP LOCKED LOOP
  IF j.attempt<3 AND NOT j.cancel_requested THEN
   UPDATE photo.video_jobs SET state='queued',claim_token=NULL,lease_until=NULL,revision=revision+1 WHERE video_jobs.tenant_id=j.tenant_id AND id=j.id;
  ELSE
   PERFORM photo.settle_video_job(j.tenant_id,j.id,CASE WHEN j.cancel_requested THEN 'cancelled' ELSE 'failed' END,
    CASE WHEN j.cancel_requested THEN NULL ELSE 'VIDEO_JOB_ABANDONED' END,NULL,NULL,'[]'::jsonb);
  END IF;
 END LOOP;
 SELECT * INTO j FROM photo.video_jobs v WHERE v.pool=p_pool AND v.state='queued' ORDER BY v.created_at,v.id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.video_jobs SET state='running',claim_token=tok,lease_until=clock_timestamp()+make_interval(secs=>p_lease_seconds),attempt=attempt+1,
  started_at=coalesce(started_at,clock_timestamp()),revision=revision+1 WHERE video_jobs.tenant_id=j.tenant_id AND id=j.id;
 IF j.kind='render' THEN UPDATE photo.video_renders r SET state='rendering',revision=revision+1 WHERE r.tenant_id=j.tenant_id AND r.job_id=j.id AND r.state='queued';END IF;
 RETURN QUERY SELECT j.id,j.tenant_id,j.project_id,j.kind,j.media_id,j.sequence_id,j.sequence_revision,j.params_artifact_id,j.params_sha256,j.backend_requested,tok,j.attempt+1;
END $$;
-- +goose StatementEnd

-- Progress heartbeat (also renews the lease). false = this claim lost the job or cancellation was requested: stop work.
-- +goose StatementBegin
CREATE FUNCTION photo.report_video_job_progress(p_tenant uuid,p_job uuid,p_token uuid,p_stage text,p_progress numeric,p_eta integer,p_lease_seconds integer) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.video_jobs%ROWTYPE;
BEGIN
 IF p_progress IS NULL OR p_progress NOT BETWEEN 0 AND 1 OR p_lease_seconds NOT BETWEEN 10 AND 600 THEN RAISE EXCEPTION 'VIDEO_PROGRESS_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO j FROM photo.video_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR j.state<>'running' OR j.claim_token IS DISTINCT FROM p_token OR j.lease_until<=clock_timestamp() THEN RETURN false;END IF;
 -- DB writes are throttled (JOB_PROGRESS_DB_INTERVAL_MS / MIN_DELTA_PCT): the lease is renewed only when something changes
 IF p_stage IS DISTINCT FROM j.stage OR p_progress-j.progress>=0.01 OR j.lease_until-clock_timestamp()<make_interval(secs=>p_lease_seconds/2.0) THEN
  UPDATE photo.video_jobs SET stage=p_stage,progress=p_progress,eta_seconds=p_eta,lease_until=clock_timestamp()+make_interval(secs=>p_lease_seconds) WHERE tenant_id=p_tenant AND id=p_job;END IF;
 RETURN NOT j.cancel_requested;
END $$;
-- +goose StatementEnd

-- Internal terminal transition (shared by finish/cancel/abandon). Outputs: [{"role","ordinal","artifact_id"}].
-- Quota: output bytes must fit the reservation (VID-Q-05); the remainder is released.
-- +goose StatementBegin
CREATE FUNCTION photo.settle_video_job(p_tenant uuid,p_job uuid,p_state text,p_error_code text,p_error_detail text,p_backend_used text,p_outputs jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.video_jobs%ROWTYPE;r photo.quota_reservations%ROWTYPE;o record;total bigint:=0;b bigint;st text:=p_state;ec text:=p_error_code;
BEGIN
 SELECT * INTO j FROM photo.video_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF j.state IN('succeeded','failed','cancelled','expired') THEN RETURN j.state=p_state;END IF;
 IF j.reservation_id IS NOT NULL THEN
  SELECT * INTO r FROM photo.quota_reservations WHERE tenant_id=p_tenant AND id=j.reservation_id AND state='reserved' AND holder_type='video_job' AND holder_id=j.id FOR UPDATE;
 END IF;
 IF st='succeeded' THEN
  FOR o IN SELECT * FROM jsonb_to_recordset(coalesce(p_outputs,'[]')) AS x(role text,ordinal integer,artifact_id uuid) LOOP
   SELECT bytes INTO b FROM photo.artifacts WHERE tenant_id=p_tenant AND id=o.artifact_id AND state='ready';
   IF NOT FOUND THEN RAISE EXCEPTION 'VIDEO_JOB_OUTPUT_MISSING' USING ERRCODE='55000';END IF;
   total:=total+b;
  END LOOP;
  IF r.id IS NOT NULL AND total>r.bytes THEN st:='failed';ec:='OUTPUT_EXCEEDS_RESERVATION';
  ELSE
   FOR o IN SELECT * FROM jsonb_to_recordset(coalesce(p_outputs,'[]')) AS x(role text,ordinal integer,artifact_id uuid) LOOP
    INSERT INTO photo.video_job_outputs(tenant_id,job_id,role,ordinal,artifact_id) VALUES(p_tenant,p_job,o.role,o.ordinal,o.artifact_id);
   END LOOP;
  END IF;
 END IF;
 IF r.id IS NOT NULL THEN
  IF st='succeeded' THEN
   UPDATE photo.quota_reservations SET state='consumed' WHERE tenant_id=p_tenant AND id=r.id;
   UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-r.bytes,storage_bytes=storage_bytes+total WHERE tenant_id=p_tenant;
  ELSE
   UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=p_tenant AND id=r.id;
   UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-r.bytes WHERE tenant_id=p_tenant;
  END IF;
 END IF;
 UPDATE photo.video_jobs SET state=st,error_code=CASE WHEN st='failed' THEN coalesce(ec,'VIDEO_JOB_FAILED') END,error_detail=CASE WHEN st='failed' THEN left(p_error_detail,512) END,
  backend_used=p_backend_used,progress=CASE WHEN st='succeeded' THEN 1 ELSE progress END,claim_token=NULL,lease_until=NULL,finished_at=clock_timestamp(),revision=revision+1
  WHERE tenant_id=p_tenant AND id=p_job;
 IF j.kind='render' THEN
  UPDATE photo.video_renders SET state=CASE st WHEN 'succeeded' THEN 'verifying' WHEN 'cancelled' THEN 'cancelled' ELSE 'failed' END,revision=revision+1
   WHERE tenant_id=p_tenant AND job_id=p_job AND state IN('queued','rendering');
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd

-- FN-D-09: worker finish, fenced by the claim token.
-- +goose StatementBegin
CREATE FUNCTION photo.finish_video_job(p_tenant uuid,p_job uuid,p_token uuid,p_state text,p_error_code text,p_error_detail text,p_backend_used text,p_outputs jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.video_jobs%ROWTYPE;
BEGIN
 IF p_state NOT IN('succeeded','failed','cancelled') OR (p_state='failed' AND (p_error_code IS NULL OR length(p_error_code)>64)) THEN RAISE EXCEPTION 'VIDEO_JOB_STATE' USING ERRCODE='22023';END IF;
 SELECT * INTO j FROM photo.video_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF j.state IN('succeeded','failed','cancelled','expired') THEN RETURN j.state=p_state AND j.claim_token IS NULL;END IF;
 IF j.state<>'running' OR j.claim_token IS DISTINCT FROM p_token THEN RETURN false;END IF;
 RETURN photo.settle_video_job(p_tenant,p_job,p_state,p_error_code,p_error_detail,p_backend_used,p_outputs);
END $$;
-- +goose StatementEnd

-- FN-D-10
-- +goose StatementBegin
CREATE FUNCTION photo.cancel_video_job(p_tenant uuid,p_job uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.video_jobs%ROWTYPE;
BEGIN
 SELECT * INTO j FROM photo.video_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'VIDEO_JOB_NOT_FOUND' USING ERRCODE='P0002';END IF;
 IF j.state='cancelled' THEN RETURN 'cancelled';END IF;
 IF j.state NOT IN('queued','running') THEN RAISE EXCEPTION 'VIDEO_JOB_NOT_CANCELLABLE' USING ERRCODE='55000';END IF;
 IF j.state='queued' THEN PERFORM photo.settle_video_job(p_tenant,p_job,'cancelled',NULL,NULL,NULL,'[]'::jsonb);RETURN 'cancelled';END IF;
 UPDATE photo.video_jobs SET cancel_requested=true,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_job;
 RETURN 'cancelling';
END $$;
-- +goose StatementEnd

-- Ownership and grants (R16). admit_video_job and settle_video_job are internal: no grants.
ALTER FUNCTION photo.video_job_cost_seconds(text,bigint) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.video_retry_after_seconds(bigint,integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.admit_video_job(uuid,text,integer,integer,bigint) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.create_video_job(uuid,uuid,uuid,text,uuid,uuid,bigint,uuid,text,text,bigint,text,bigint,integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.create_video_render(uuid,uuid,uuid,uuid,bigint,jsonb,text,uuid,text,text,bigint,text,bigint,integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.claim_video_job(text,integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.report_video_job_progress(uuid,uuid,uuid,text,numeric,integer,integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.settle_video_job(uuid,uuid,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.finish_video_job(uuid,uuid,uuid,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.cancel_video_job(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.video_job_cost_seconds(text,bigint),photo.video_retry_after_seconds(bigint,integer),photo.admit_video_job(uuid,text,integer,integer,bigint),
 photo.create_video_job(uuid,uuid,uuid,text,uuid,uuid,bigint,uuid,text,text,bigint,text,bigint,integer),
 photo.create_video_render(uuid,uuid,uuid,uuid,bigint,jsonb,text,uuid,text,text,bigint,text,bigint,integer),
 photo.claim_video_job(text,integer),photo.report_video_job_progress(uuid,uuid,uuid,text,numeric,integer,integer),
 photo.settle_video_job(uuid,uuid,text,text,text,text,jsonb),photo.finish_video_job(uuid,uuid,uuid,text,text,text,text,jsonb),
 photo.cancel_video_job(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.video_job_cost_seconds(text,bigint),photo.video_retry_after_seconds(bigint,integer),
 photo.create_video_job(uuid,uuid,uuid,text,uuid,uuid,bigint,uuid,text,text,bigint,text,bigint,integer),
 photo.create_video_render(uuid,uuid,uuid,uuid,bigint,jsonb,text,uuid,text,text,bigint,text,bigint,integer),photo.cancel_video_job(uuid,uuid) TO photo_api;
GRANT EXECUTE ON FUNCTION photo.claim_video_job(text,integer),photo.report_video_job_progress(uuid,uuid,uuid,text,numeric,integer,integer),
 photo.finish_video_job(uuid,uuid,uuid,text,text,text,text,jsonb) TO photo_worker;
GRANT EXECUTE ON FUNCTION photo.video_job_cost_seconds(text,bigint),photo.create_video_job(uuid,uuid,uuid,text,uuid,uuid,bigint,uuid,text,text,bigint,text,bigint,integer) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
