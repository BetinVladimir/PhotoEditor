-- +goose Up
ALTER TABLE photo.upload_sessions ADD COLUMN completion_token uuid;
ALTER TABLE photo.upload_sessions ADD COLUMN completion_lease_until timestamptz;
ALTER TABLE photo.upload_sessions ADD COLUMN next_completion_at timestamptz;
ALTER TABLE photo.upload_sessions ADD COLUMN staging_candidate_id uuid;
ALTER TABLE photo.upload_sessions ADD COLUMN ingest_snapshot_id uuid;
ALTER TABLE photo.upload_sessions ADD COLUMN ingest_run_id uuid;
ALTER TABLE photo.upload_sessions ADD CONSTRAINT upload_completion_lease_pair
 CHECK((completion_token IS NULL)=(completion_lease_until IS NULL));
ALTER TABLE photo.upload_sessions ADD CONSTRAINT upload_completion_ids_pair
 CHECK((staging_candidate_id IS NULL)=(ingest_snapshot_id IS NULL) AND (ingest_snapshot_id IS NULL)=(ingest_run_id IS NULL));
CREATE INDEX upload_completion_pending ON photo.upload_sessions(next_completion_at,id) WHERE state='completing';

-- +goose StatementBegin
CREATE FUNCTION photo.claim_upload_completion()
RETURNS TABLE(upload_id uuid,completion_token uuid,tenant_id uuid,object_key text,multipart_id text,
 media_type text,filename text,assume_profile text,expected_bytes bigint,expected_sha text,artifact_id uuid,snapshot_id uuid,run_id uuid,parts jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.upload_sessions%ROWTYPE;u photo.upload_sessions%ROWTYPE;fence uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO hint FROM photo.upload_sessions x WHERE x.state='completing' AND x.next_completion_at<=clock_timestamp()
 AND (x.completion_lease_until IS NULL OR x.completion_lease_until<=clock_timestamp())
 ORDER BY x.next_completion_at,x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 SELECT * INTO u FROM photo.upload_sessions x WHERE x.id=hint.id AND x.tenant_id=hint.tenant_id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR u.multipart_id IS NULL OR u.media_type IS NULL OR u.staging_candidate_id IS NULL
 OR u.ingest_snapshot_id IS NULL OR u.ingest_run_id IS NULL OR u.actual_size IS NOT NULL OR u.staging_artifact_id IS NOT NULL
 OR u.expires_at<=clock_timestamp() THEN RETURN;END IF;
 IF (SELECT count(*) FROM photo.upload_parts p WHERE p.tenant_id=u.tenant_id AND p.upload_session_id=u.id AND p.state='completed')<1 THEN RETURN;END IF;
 fence:=gen_random_uuid();
 UPDATE photo.upload_sessions SET completion_token=fence,completion_lease_until=clock_timestamp()+interval '60 seconds',
  next_completion_at=clock_timestamp()+interval '60 seconds' WHERE id=u.id;
 RETURN QUERY SELECT u.id,fence,u.tenant_id,u.object_key,u.multipart_id,u.media_type,u.filename,u.assume_profile,u.expected_size,u.expected_sha256,
  u.staging_candidate_id,u.ingest_snapshot_id,u.ingest_run_id,
  (SELECT jsonb_agg(jsonb_build_object('part_no',p.part_no,'etag',p.etag) ORDER BY p.part_no)
   FROM photo.upload_parts p WHERE p.tenant_id=u.tenant_id AND p.upload_session_id=u.id AND p.state='completed');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_upload_completion() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_upload_completion() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_upload_completion() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.renew_upload_completion(p_upload uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ BEGIN
 UPDATE photo.upload_sessions SET completion_lease_until=clock_timestamp()+interval '60 seconds'
 WHERE id=p_upload AND state='completing' AND completion_token=p_token AND completion_lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_upload_completion(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_upload_completion(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_upload_completion(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.release_upload_completion(p_upload uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ BEGIN
 UPDATE photo.upload_sessions SET completion_token=NULL,completion_lease_until=NULL,next_completion_at=clock_timestamp()+interval '5 seconds'
 WHERE id=p_upload AND state='completing' AND completion_token=p_token;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_upload_completion(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_upload_completion(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_upload_completion(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.finish_upload_completion(p_upload uuid,p_token uuid,p_bucket text,p_version text,p_sha text,p_payload jsonb,
 p_snapshot_digest text,p_config jsonb,p_config_hash text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE u photo.upload_sessions%ROWTYPE;
BEGIN
 IF p_token IS NULL OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 OR p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_snapshot_digest IS NULL OR p_snapshot_digest !~ '^[0-9a-f]{64}$'
 OR p_config_hash IS NULL OR p_config_hash !~ '^[0-9a-f]{64}$' OR p_payload IS NULL OR jsonb_typeof(p_payload)<>'object'
 OR p_config IS NULL OR jsonb_typeof(p_config)<>'object' THEN RETURN false;END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE id=p_upload FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF u.state='verifying' THEN
  RETURN u.staging_artifact_id=u.staging_candidate_id AND u.actual_size=u.expected_size
   AND EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=u.tenant_id AND a.id=u.staging_artifact_id AND a.state='ready' AND a.object_version=p_version AND a.sha256=p_sha)
   AND EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=u.tenant_id AND r.id=u.ingest_run_id AND r.kind='ingest');
 END IF;
 IF u.state<>'completing' OR u.completion_token IS DISTINCT FROM p_token OR u.completion_lease_until IS NULL OR u.completion_lease_until<=clock_timestamp()
 OR u.multipart_id IS NULL OR u.media_type IS NULL OR u.staging_candidate_id IS NULL OR u.ingest_snapshot_id IS NULL OR u.ingest_run_id IS NULL
 OR u.principal_type<>'user' OR u.actual_size IS NOT NULL OR u.staging_artifact_id IS NOT NULL OR u.expires_at<=clock_timestamp() THEN RETURN false;END IF;
 IF (p_payload-'schema_version'-'suffix'-'assume_profile'-'analyze'-'tile')<>'{}' OR p_payload->'schema_version'<>'1'::jsonb
 OR p_payload->'analyze'<>'true'::jsonb OR p_payload->'tile'<>'512'::jsonb OR jsonb_typeof(p_payload->'suffix')<>'string'
 OR (p_payload->'assume_profile'<>'null'::jsonb AND p_payload->>'assume_profile' NOT IN ('srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'))
 OR (p_config-'schema_version'-'source')<>'{}' OR p_config->'schema_version'<>'1'::jsonb OR p_config->>'source'<>'upload' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.quota_reservations q WHERE q.tenant_id=u.tenant_id AND q.id=u.reservation_id AND q.state='reserved'
  AND q.holder_type='upload' AND q.holder_id=u.id AND q.bytes=u.expected_size AND q.files=1) THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role,expires_at)
 SELECT u.staging_candidate_id,u.tenant_id,p_bucket,u.object_key,p_version,p_sha,u.expected_size,u.media_type,
  'ready',clock_timestamp(),'staging_upload',clock_timestamp()+interval '30 days'
 ;
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at)
 VALUES(u.ingest_snapshot_id,u.tenant_id,'input',u.principal_type,u.principal_id,p_snapshot_digest,1,clock_timestamp()+interval '30 days');
 INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload)
 VALUES(u.tenant_id,u.ingest_snapshot_id,0,u.staging_candidate_id,p_sha,p_payload);
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by)
 VALUES(u.ingest_run_id,u.tenant_id,'ingest','queued',u.ingest_snapshot_id,p_config,p_config_hash,u.principal_id);
 UPDATE photo.upload_sessions SET state='verifying',actual_size=expected_size,staging_artifact_id=staging_candidate_id,
  completion_token=NULL,completion_lease_until=NULL,next_completion_at=NULL,revision=revision+1 WHERE id=u.id;
 IF NOT EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=u.tenant_id AND p.id=u.project_id AND p.archived_at IS NULL)
 OR (u.collection_id IS NOT NULL AND NOT EXISTS(SELECT FROM photo.collections c WHERE c.tenant_id=u.tenant_id AND c.id=u.collection_id AND c.project_id=u.project_id AND c.deleted_at IS NULL)) THEN
  RAISE EXCEPTION 'INGEST_DESTINATION_MISSING' USING ERRCODE='23514';
 END IF;
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
