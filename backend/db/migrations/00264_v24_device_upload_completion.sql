-- Allow asynchronous completion of a device upload accepted while its credential
-- was active. Revocation fences new API mutations, not an already queued ingest.
-- +goose Up
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
 IF (p_payload-'schema_version'-'suffix'-'assume_profile'-'analyze'-'tile')<>'{}' OR p_payload->'schema_version'<>'1'::jsonb OR p_payload->'analyze'<>'true'::jsonb
 OR p_payload->'tile'<>'512'::jsonb OR jsonb_typeof(p_payload->'suffix')<>'string' OR (p_payload->'assume_profile'<>'null'::jsonb AND p_payload->>'assume_profile' NOT IN ('srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'))
 OR (p_config-'schema_version'-'source')<>'{}' OR p_config->'schema_version'<>'1'::jsonb OR p_config->>'source'<>'upload' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.quota_reservations q WHERE q.tenant_id=u.tenant_id AND q.id=u.reservation_id AND q.state='reserved' AND q.holder_type='upload' AND q.holder_id=u.id AND q.bytes=u.expected_size AND q.files=1) THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role,expires_at)
 VALUES(u.staging_candidate_id,u.tenant_id,p_bucket,u.object_key,p_version,p_sha,u.expected_size,u.media_type,'ready',clock_timestamp(),'staging_upload',clock_timestamp()+interval '30 days');
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(u.ingest_snapshot_id,u.tenant_id,'input',u.principal_type,u.principal_id,p_snapshot_digest,1,clock_timestamp()+interval '30 days');
 INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload) VALUES(u.tenant_id,u.ingest_snapshot_id,0,u.staging_candidate_id,p_sha,p_payload);
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(u.ingest_run_id,u.tenant_id,'ingest','queued',u.ingest_snapshot_id,p_config,p_config_hash,creator);
 UPDATE photo.upload_sessions SET state='verifying',actual_size=expected_size,staging_artifact_id=staging_candidate_id,completion_token=NULL,completion_lease_until=NULL,next_completion_at=NULL,revision=revision+1 WHERE id=u.id;
 IF NOT EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=u.tenant_id AND p.id=u.project_id AND p.archived_at IS NULL)
 OR u.collection_id IS NULL OR NOT EXISTS(SELECT FROM photo.collections c WHERE c.tenant_id=u.tenant_id AND c.id=u.collection_id AND c.project_id=u.project_id AND c.deleted_at IS NULL) THEN RAISE EXCEPTION 'INGEST_DESTINATION_MISSING' USING ERRCODE='23514';END IF;
 INSERT INTO photo.ingest_upload_bindings(tenant_id,run_id,ordinal,upload_id,source_id,source_sha,project_id,collection_id,principal_type,principal_id)
 VALUES(u.tenant_id,u.ingest_run_id,0,u.id,u.staging_candidate_id,p_sha,u.project_id,u.collection_id,u.principal_type,u.principal_id);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text) TO photo_scheduler;

-- Expected-hash mismatch persists the actual object for deterministic cleanup.
-- Guest share counters move reserved -> used in the same transaction.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.fail_upload_completion(p_upload uuid,p_token uuid,p_bucket text,p_version text,p_sha text,p_payload jsonb,
 p_snapshot_digest text,p_config jsonb,p_config_hash text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.upload_sessions%ROWTYPE;u photo.upload_sessions%ROWTYPE;q photo.quota_reservations%ROWTYPE;l photo.share_links%ROWTYPE;reserved bigint;creator uuid;
BEGIN
 IF p_token IS NULL OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 OR p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_snapshot_digest IS NULL OR p_snapshot_digest !~ '^[0-9a-f]{64}$' OR p_config_hash IS NULL OR p_config_hash !~ '^[0-9a-f]{64}$'
 OR p_payload IS NULL OR jsonb_typeof(p_payload)<>'object' OR p_config IS NULL OR jsonb_typeof(p_config)<>'object' THEN RETURN false;END IF;
 SELECT * INTO hint FROM photo.upload_sessions WHERE id=p_upload;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT reserved_bytes INTO reserved FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 IF hint.share_id IS NOT NULL THEN SELECT * INTO l FROM photo.share_links WHERE tenant_id=hint.tenant_id AND id=hint.share_id FOR UPDATE;END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE id=p_upload AND tenant_id=hint.tenant_id FOR UPDATE;
 IF u.state='rejected' THEN RETURN u.staging_artifact_id=u.staging_candidate_id AND u.actual_size=u.expected_size
  AND EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=u.tenant_id AND a.id=u.staging_artifact_id AND a.object_version=p_version AND a.sha256=p_sha);END IF;
 IF u.state<>'completing' OR u.completion_token IS DISTINCT FROM p_token OR u.completion_lease_until IS NULL OR u.completion_lease_until<=clock_timestamp()
 OR u.principal_type NOT IN ('user','guest','device') OR u.expected_sha256 IS NULL OR u.expected_sha256=p_sha OR u.staging_candidate_id IS NULL
 OR u.ingest_snapshot_id IS NULL OR u.ingest_run_id IS NULL OR u.actual_size IS NOT NULL OR u.staging_artifact_id IS NOT NULL
 OR (u.principal_type='guest' AND u.share_id IS DISTINCT FROM l.id)
 OR (u.principal_type<>'guest' AND u.share_id IS NOT NULL) THEN RETURN false;END IF;
 IF u.principal_type='user' THEN creator:=u.principal_id;
 ELSIF u.principal_type='device' THEN
  SELECT created_by INTO creator FROM photo.capture_devices
   WHERE tenant_id=u.tenant_id AND project_id=u.project_id AND id=u.principal_id;
 ELSE creator:=l.created_by;END IF;
 IF creator IS NULL THEN RETURN false;END IF;
 IF (p_payload-'schema_version'-'suffix'-'assume_profile'-'analyze'-'tile')<>'{}' OR p_payload->'schema_version'<>'1'::jsonb OR p_payload->'analyze'<>'true'::jsonb
 OR p_payload->'tile'<>'512'::jsonb OR jsonb_typeof(p_payload->'suffix')<>'string' OR (p_payload->'assume_profile'<>'null'::jsonb AND p_payload->>'assume_profile' NOT IN ('srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'))
 OR (p_config-'schema_version'-'source')<>'{}' OR p_config->'schema_version'<>'1'::jsonb OR p_config->>'source'<>'upload' THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=u.tenant_id AND id=u.reservation_id FOR UPDATE;
 IF NOT FOUND OR q.state<>'reserved' OR q.holder_type<>'upload' OR q.holder_id<>u.id OR q.bytes<>u.expected_size OR q.files<>1 OR reserved<q.bytes
 OR (l.id IS NOT NULL AND (l.reserved_bytes<q.bytes OR l.reserved_files<q.files OR l.used_bytes>9007199254740991-u.expected_size OR l.used_files>9007199254740991-q.files)) THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role,expires_at)
 VALUES(u.staging_candidate_id,u.tenant_id,p_bucket,u.object_key,p_version,p_sha,u.expected_size,u.media_type,'ready',clock_timestamp(),'staging_upload',clock_timestamp()+interval '30 days');
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(u.ingest_snapshot_id,u.tenant_id,'input',u.principal_type,u.principal_id,p_snapshot_digest,1,clock_timestamp()+interval '30 days');
 INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload) VALUES(u.tenant_id,u.ingest_snapshot_id,0,u.staging_candidate_id,p_sha,p_payload);
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(u.ingest_run_id,u.tenant_id,'ingest','failed',u.ingest_snapshot_id,p_config,p_config_hash,creator,true);
 INSERT INTO photo.ingest_progress(tenant_id,run_id,phase,item_count) VALUES(u.tenant_id,u.ingest_run_id,'complete',1);
 INSERT INTO photo.ingest_upload_bindings(tenant_id,run_id,ordinal,upload_id,source_id,source_sha,project_id,collection_id,principal_type,principal_id)
 VALUES(u.tenant_id,u.ingest_run_id,0,u.id,u.staging_candidate_id,p_sha,u.project_id,u.collection_id,u.principal_type,u.principal_id);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes,storage_bytes=storage_bytes+u.expected_size WHERE tenant_id=u.tenant_id AND reserved_bytes>=q.bytes AND storage_bytes<=9007199254740991-u.expected_size;
 IF NOT FOUND THEN RAISE EXCEPTION 'FAILED_UPLOAD_BYTE_ACCOUNTING' USING ERRCODE='23514';END IF;
 IF l.id IS NOT NULL THEN
  UPDATE photo.share_links SET reserved_bytes=reserved_bytes-q.bytes,reserved_files=reserved_files-q.files,used_bytes=used_bytes+u.expected_size,used_files=used_files+q.files WHERE tenant_id=u.tenant_id AND id=l.id;
 END IF;
 UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=u.tenant_id AND id=q.id;
 UPDATE photo.upload_sessions SET state='rejected',actual_size=expected_size,staging_artifact_id=staging_candidate_id,completion_token=NULL,completion_lease_until=NULL,next_completion_at=NULL,revision=revision+1 WHERE id=u.id;
 INSERT INTO photo.ingest_rejections(tenant_id,run_id,ordinal,failure_stage,failure_reason,staging_bytes) VALUES(u.tenant_id,u.ingest_run_id,0,'upload.verify','failed',u.expected_size);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.fail_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.fail_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
