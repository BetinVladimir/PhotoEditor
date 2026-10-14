-- +goose Up
ALTER TABLE photo.v30_archive_begin_generations ADD COLUMN cleanup_token uuid;
ALTER TABLE photo.v30_archive_begin_generations ADD COLUMN cleanup_lease_until timestamptz;
ALTER TABLE photo.v30_archive_begin_generations ADD COLUMN purged_at timestamptz;
CREATE INDEX v30_archive_begin_generations_cleanup
 ON photo.v30_archive_begin_generations(state,created_at,upload_id)
 WHERE state IN ('created','bound','aborting');

-- Claim only a terminal successful Create with a closed matching writer. An
-- unknown Create remains 'creating' and cannot reach physical cleanup.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_v30_archive_generation_cleanup()
RETURNS TABLE(tenant_id uuid,upload_id uuid,generation_id uuid,cleanup_token uuid,bucket text,object_key text,multipart_id text,cleanup_lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;g photo.v30_archive_begin_generations%ROWTYPE;fence uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO d FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id
 WHERE x.state='aborting' AND x.error_code IN('ARCHIVE_INTEGRITY','UPLOAD_EXPIRED','UPLOAD_ABORTED')
 AND x.expires_at<=clock_timestamp() AND x.cleanup_not_before<=clock_timestamp()
 AND (x.parts_authorized_until IS NULL OR x.parts_authorized_until<=clock_timestamp()) AND x.completion_lease_until IS NULL
 AND t.status='active' AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=x.tenant_id AND w.upload_id=x.id AND w.state='open')
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND (a.id=x.artifact_id OR (a.bucket=x.bucket AND a.object_key=x.object_key)))
 AND EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations q WHERE q.tenant_id=x.tenant_id AND q.upload_id=x.id
  AND q.state IN('created','bound','aborting') AND (q.cleanup_lease_until IS NULL OR q.cleanup_lease_until<=clock_timestamp()))
 ORDER BY x.cleanup_not_before,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT q.* INTO g FROM photo.v30_archive_begin_generations q
 WHERE q.tenant_id=d.tenant_id AND q.upload_id=d.id AND q.state IN('created','bound','aborting')
 AND (q.cleanup_lease_until IS NULL OR q.cleanup_lease_until<=clock_timestamp())
 AND q.multipart_id IS NOT NULL AND q.terminal_evidence='s3-create-receipt'
 AND EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=q.tenant_id AND w.upload_id=q.upload_id
  AND w.token=q.writer_token AND w.kind='begin' AND w.state='closed' AND w.receipt=q.multipart_id)
 ORDER BY q.created_at,q.generation_id FOR UPDATE OF q SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.v30_archive_begin_generations q SET state='aborting',cleanup_token=fence,
 cleanup_lease_until=clock_timestamp()+interval '2 minutes',revision=q.revision+1
 WHERE q.tenant_id=g.tenant_id AND q.upload_id=g.upload_id AND q.generation_id=g.generation_id RETURNING * INTO g;
 RETURN QUERY SELECT g.tenant_id,g.upload_id,g.generation_id,g.cleanup_token,d.bucket,g.object_key,g.multipart_id,g.cleanup_lease_until;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.v30_archive_generation_cleanup_authorized(p_tenant uuid,p_upload uuid,p_generation uuid,p_token uuid,p_bucket text,p_key text,p_multipart text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_token IS NOT NULL AND EXISTS(
 SELECT 1 FROM photo.v30_archive_begin_generations g JOIN photo.v30_archive_uploads x
 ON x.tenant_id=g.tenant_id AND x.id=g.upload_id JOIN photo.tenants t ON t.id=x.tenant_id
 JOIN photo.v30_archive_writers w ON w.tenant_id=g.tenant_id AND w.upload_id=g.upload_id AND w.token=g.writer_token
 WHERE g.tenant_id=p_tenant AND g.upload_id=p_upload AND g.generation_id=p_generation
 AND g.state='aborting' AND g.cleanup_token=p_token AND g.cleanup_lease_until>clock_timestamp()
 AND g.object_key=p_key AND g.multipart_id=p_multipart AND g.terminal_evidence='s3-create-receipt'
 AND w.kind='begin' AND w.state='closed' AND w.receipt=p_multipart
 AND x.state='aborting' AND x.error_code IN('ARCHIVE_INTEGRITY','UPLOAD_EXPIRED','UPLOAD_ABORTED')
 AND x.bucket=p_bucket AND x.expires_at<=clock_timestamp() AND x.cleanup_not_before<=clock_timestamp()
 AND (x.parts_authorized_until IS NULL OR x.parts_authorized_until<=clock_timestamp()) AND x.completion_lease_until IS NULL
 AND t.status='active' AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers z WHERE z.tenant_id=x.tenant_id AND z.upload_id=x.id AND z.state='open')
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND (a.id=x.artifact_id OR (a.bucket=x.bucket AND a.object_key=g.object_key)))
 );
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.renew_v30_archive_generation_cleanup(p_tenant uuid,p_upload uuid,p_generation uuid,p_token uuid,p_bucket text,p_key text,p_multipart text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v30_archive_generation_cleanup_authorized(p_tenant,p_upload,p_generation,p_token,p_bucket,p_key,p_multipart) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.v30_archive_begin_generations SET cleanup_lease_until=clock_timestamp()+interval '2 minutes'
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation AND cleanup_token=p_token AND state='aborting';
 RETURN FOUND;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.release_v30_archive_generation_cleanup(p_tenant uuid,p_upload uuid,p_generation uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 UPDATE photo.v30_archive_begin_generations SET cleanup_token=NULL,cleanup_lease_until=NULL
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation
 AND state='aborting' AND cleanup_token=p_token AND cleanup_lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd

-- Scheduler calls Finish only after exact-key physical purge and renewed
-- absence checks. This transaction does not infer storage state from SQL.
-- +goose StatementBegin
CREATE FUNCTION photo.finish_v30_archive_generation_cleanup(p_tenant uuid,p_upload uuid,p_generation uuid,p_token uuid,p_bucket text,p_key text,p_multipart text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 IF photo.v30_archive_generation_cleanup_authorized(p_tenant,p_upload,p_generation,p_token,p_bucket,p_key,p_multipart) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.v30_archive_begin_generations SET state='aborted',cleanup_lease_until=NULL,purged_at=clock_timestamp(),revision=revision+1
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation AND cleanup_token=p_token AND state='aborting';
 RETURN FOUND;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.claim_v30_archive_generation_cleanup() OWNER TO photo_fn_owner;
ALTER FUNCTION photo.v30_archive_generation_cleanup_authorized(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.renew_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.release_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.finish_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v30_archive_generation_cleanup() FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
REVOKE ALL ON FUNCTION photo.v30_archive_generation_cleanup_authorized(uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
REVOKE ALL ON FUNCTION photo.renew_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
REVOKE ALL ON FUNCTION photo.release_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
REVOKE ALL ON FUNCTION photo.finish_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.claim_v30_archive_generation_cleanup() TO photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.v30_archive_generation_cleanup_authorized(uuid,uuid,uuid,uuid,text,text,text) TO photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.renew_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid,text,text,text) TO photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.release_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid) TO photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.finish_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid,text,text,text) TO photo_scheduler;

-- Existing parent cleanup must wait for every generation's physical cleanup.
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;
BEGIN
 SELECT pg_get_functiondef('photo.claim_v30_archive_cleanup()'::regprocedure) INTO definition;
 needle:=' AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=x.tenant_id AND w.upload_id=x.id AND w.state=''open'')';
 IF definition IS NULL OR position(needle IN definition)=0 OR position('v30_archive_begin_generations' IN definition)>0 THEN RAISE EXCEPTION 'ARCHIVE_PARENT_GENERATION_CLAIM_SHAPE';END IF;
 EXECUTE replace(definition,needle,
  ' AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g WHERE g.tenant_id=x.tenant_id AND g.upload_id=x.id AND g.state<>''aborted'')'||needle);
 SELECT pg_get_functiondef('photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text)'::regprocedure) INTO definition;
 needle:=' AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=x.tenant_id AND w.upload_id=x.id AND w.state=''open'')';
 IF definition IS NULL OR position(needle IN definition)=0 OR position('v30_archive_begin_generations' IN definition)>0 THEN RAISE EXCEPTION 'ARCHIVE_PARENT_GENERATION_AUTH_SHAPE';END IF;
 EXECUTE replace(definition,needle,
  ' AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g WHERE g.tenant_id=x.tenant_id AND g.upload_id=x.id AND g.state<>''aborted'')'||needle);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
