-- +goose Up
-- Worker-facing access for video jobs. photo_worker has no table privileges on photo.artifacts, so a running job
-- (valid claim token) registers its outputs through register_video_artifact and reads its inputs through
-- get_video_job_inputs. finish_video_ingest / finish_video_render are additionally granted to photo_worker: the
-- DEMO worker settles the domain row, then calls finish_video_job (fence-checked) to settle the job.

-- +goose StatementBegin
CREATE FUNCTION photo.register_video_artifact(p_tenant uuid,p_job uuid,p_token uuid,p_role text,p_bucket text,p_key text,p_sha text,
 p_bytes bigint,p_media_type text,p_width integer,p_height integer) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.video_jobs%ROWTYPE;a photo.artifacts%ROWTYPE;aid uuid;
BEGIN
 IF p_role NOT IN('video_probe','video_proxy','video_filmstrip','video_waveform','video_render','video_qc_report')
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_bytes IS NULL OR p_bytes<0 OR p_media_type IS NULL OR length(p_media_type) NOT BETWEEN 3 AND 127
  OR p_key IS NULL OR left(p_key,length('tenants/'||p_tenant::text||'/video/'))<>'tenants/'||p_tenant::text||'/video/' OR length(p_key)>1024 THEN
  RAISE EXCEPTION 'VIDEO_ARTIFACT_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO j FROM photo.video_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR j.state<>'running' OR j.claim_token IS DISTINCT FROM p_token THEN
  RAISE EXCEPTION 'VIDEO_JOB_NOT_RUNNING' USING ERRCODE='55000';END IF;
 IF (p_role='video_render' OR p_role='video_qc_report') <> (j.kind='render') THEN
  RAISE EXCEPTION 'VIDEO_ARTIFACT_ROLE_FOR_KIND' USING ERRCODE='22023';END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND bucket=p_bucket AND object_key=p_key;
 IF FOUND THEN
  IF a.sha256=p_sha AND a.bytes=p_bytes AND a.output_role=p_role THEN RETURN a.id;END IF;
  RAISE EXCEPTION 'VIDEO_ARTIFACT_CONFLICT' USING ERRCODE='23505';END IF;
 INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,width,height,state,verified_at,output_role)
  VALUES(p_tenant,p_bucket,p_key,p_sha,p_bytes,p_media_type,p_width,p_height,'ready',clock_timestamp(),p_role) RETURNING id INTO aid;
 RETURN aid;
END $$;
-- +goose StatementEnd

-- Inputs of a running job: source objects by media, and for render the frozen sequence document and render row.
-- +goose StatementBegin
CREATE FUNCTION photo.get_video_job_inputs(p_tenant uuid,p_job uuid,p_token uuid) RETURNS jsonb
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.video_jobs%ROWTYPE;res jsonb;
BEGIN
 SELECT * INTO j FROM photo.video_jobs WHERE tenant_id=p_tenant AND id=p_job;
 IF NOT FOUND OR j.state<>'running' OR j.claim_token IS DISTINCT FROM p_token THEN
  RAISE EXCEPTION 'VIDEO_JOB_NOT_RUNNING' USING ERRCODE='55000';END IF;
 res:=jsonb_build_object('job_id',j.id,'kind',j.kind,'project_id',j.project_id,'backend_requested',j.backend_requested);
 IF j.kind='ingest' THEN
  res:=res||jsonb_build_object('media',coalesce((SELECT jsonb_agg(jsonb_build_object('id',m.id,'kind',m.kind,'name',m.name,'bucket',a.bucket,
    'object_key',a.object_key,'sha256',a.sha256,'bytes',a.bytes,'probe',m.probe)) FROM photo.video_media m
    JOIN photo.artifacts a ON a.tenant_id=m.tenant_id AND a.id=m.source_artifact_id
    WHERE m.tenant_id=p_tenant AND m.id=j.media_id),'[]'::jsonb));
 ELSIF j.kind='render' THEN
  res:=res||jsonb_build_object(
   'render',(SELECT jsonb_build_object('id',r.id,'preset',r.preset,'sequence_id',r.sequence_id,'sequence_revision',r.sequence_revision,'document_sha256',r.document_sha256)
     FROM photo.video_renders r WHERE r.tenant_id=p_tenant AND r.job_id=j.id),
   'document',(SELECT jsonb_build_object('bucket',a.bucket,'object_key',a.object_key,'sha256',a.sha256,'bytes',a.bytes)
     FROM photo.video_sequence_revisions v JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.document_artifact_id
     WHERE v.tenant_id=p_tenant AND v.sequence_id=j.sequence_id AND v.revision=j.sequence_revision),
   'media',coalesce((SELECT jsonb_agg(jsonb_build_object('id',m.id,'kind',m.kind,'bucket',a.bucket,'object_key',a.object_key,'sha256',a.sha256,
     'bytes',a.bytes,'probe',m.probe)) FROM (SELECT * FROM photo.video_media WHERE tenant_id=p_tenant AND project_id=j.project_id
       AND state='ready' AND withdrawn_at IS NULL ORDER BY created_at LIMIT 500) m
     JOIN photo.artifacts a ON a.tenant_id=m.tenant_id AND a.id=m.source_artifact_id),'[]'::jsonb));
 END IF;
 RETURN res;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.register_video_artifact(uuid,uuid,uuid,text,text,text,text,bigint,text,integer,integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.get_video_job_inputs(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_video_artifact(uuid,uuid,uuid,text,text,text,text,bigint,text,integer,integer),photo.get_video_job_inputs(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_video_artifact(uuid,uuid,uuid,text,text,text,text,bigint,text,integer,integer),photo.get_video_job_inputs(uuid,uuid,uuid) TO photo_worker;
GRANT EXECUTE ON FUNCTION photo.finish_video_ingest(uuid,uuid,text,text,jsonb,uuid,uuid,uuid,uuid),
 photo.finish_video_render(uuid,uuid,text,uuid,uuid,bigint,text,text,text) TO photo_worker;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
