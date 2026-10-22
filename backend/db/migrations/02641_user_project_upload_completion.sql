-- +goose Up
-- Restore the existing user project-only upload contract (00078), while
-- preserving mandatory collection destinations for guest/device submissions.
-- No pending upload/snapshot is rewritten; standard completion retries adopt it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_upload_completion(p_upload uuid,p_token uuid,p_bucket text,p_version text,p_sha text,p_payload jsonb,
 p_snapshot_digest text,p_config jsonb,p_config_hash text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE u photo.upload_sessions%ROWTYPE;creator uuid;
BEGIN
 IF p_token IS NULL OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 OR p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_snapshot_digest IS NULL OR p_snapshot_digest !~ '^[0-9a-f]{64}$' OR p_config_hash IS NULL OR p_config_hash !~ '^[0-9a-f]{64}$'
 OR p_payload IS NULL OR jsonb_typeof(p_payload)<>'object' OR p_config IS NULL OR jsonb_typeof(p_config)<>'object' THEN RETURN false;END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE id=p_upload FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF p_payload IS DISTINCT FROM (jsonb_build_object('schema_version',CASE WHEN u.raw_development IS NULL THEN 1 ELSE 2 END,
  'suffix',lower(substring(u.filename from '\.[^.]+$')),'assume_profile',u.assume_profile,'analyze',true,'tile',512)
  || CASE WHEN u.raw_development IS NULL THEN '{}'::jsonb ELSE jsonb_build_object('raw_development',u.raw_development) END)
 OR p_config IS DISTINCT FROM '{"schema_version":1,"source":"upload"}'::jsonb THEN RETURN false;END IF;
 IF u.state='verifying' THEN RETURN u.staging_artifact_id=u.staging_candidate_id AND u.actual_size=u.expected_size
  AND EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=u.tenant_id AND a.id=u.staging_artifact_id AND a.state='ready' AND a.object_version=p_version AND a.sha256=p_sha)
  AND EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=u.tenant_id AND r.id=u.ingest_run_id AND r.kind='ingest');END IF;
 IF u.state<>'completing' OR u.completion_token IS DISTINCT FROM p_token OR u.completion_lease_until IS NULL OR u.completion_lease_until<=clock_timestamp()
 OR u.multipart_id IS NULL OR u.media_type IS NULL OR u.staging_candidate_id IS NULL OR u.ingest_snapshot_id IS NULL OR u.ingest_run_id IS NULL
 OR u.principal_type NOT IN ('user','guest','device') OR u.actual_size IS NOT NULL OR u.staging_artifact_id IS NOT NULL THEN RETURN false;END IF;
 IF u.principal_type='user' THEN creator:=u.principal_id;
 ELSIF u.principal_type='device' THEN
  SELECT created_by INTO creator FROM photo.capture_devices
   WHERE tenant_id=u.tenant_id AND project_id=u.project_id AND id=u.principal_id;
 ELSE SELECT created_by INTO creator FROM photo.share_links WHERE tenant_id=u.tenant_id AND id=u.share_id;END IF;
 IF creator IS NULL THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.quota_reservations q WHERE q.tenant_id=u.tenant_id AND q.id=u.reservation_id AND q.state='reserved' AND q.holder_type='upload' AND q.holder_id=u.id AND q.bytes=u.expected_size AND q.files=1) THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role,expires_at)
 VALUES(u.staging_candidate_id,u.tenant_id,p_bucket,u.object_key,p_version,p_sha,u.expected_size,u.media_type,'ready',clock_timestamp(),'staging_upload',clock_timestamp()+interval '30 days');
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(u.ingest_snapshot_id,u.tenant_id,'input',u.principal_type,u.principal_id,p_snapshot_digest,1,clock_timestamp()+interval '30 days');
 INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload) VALUES(u.tenant_id,u.ingest_snapshot_id,0,u.staging_candidate_id,p_sha,p_payload);
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(u.ingest_run_id,u.tenant_id,'ingest','queued',u.ingest_snapshot_id,p_config,p_config_hash,creator);
 UPDATE photo.upload_sessions SET state='verifying',actual_size=expected_size,staging_artifact_id=staging_candidate_id,completion_token=NULL,completion_lease_until=NULL,next_completion_at=NULL,revision=revision+1 WHERE id=u.id;
 IF NOT EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=u.tenant_id AND p.id=u.project_id AND p.archived_at IS NULL)
 OR (u.collection_id IS NULL AND u.principal_type<>'user')
 OR (u.collection_id IS NOT NULL AND NOT EXISTS(SELECT FROM photo.collections c WHERE c.tenant_id=u.tenant_id AND c.id=u.collection_id AND c.project_id=u.project_id AND c.deleted_at IS NULL)) THEN RAISE EXCEPTION 'INGEST_DESTINATION_MISSING' USING ERRCODE='23514';END IF;
 INSERT INTO photo.ingest_upload_bindings(tenant_id,run_id,ordinal,upload_id,source_id,source_sha,project_id,collection_id,principal_type,principal_id)
 VALUES(u.tenant_id,u.ingest_run_id,0,u.id,u.staging_candidate_id,p_sha,u.project_id,u.collection_id,u.principal_type,u.principal_id);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
