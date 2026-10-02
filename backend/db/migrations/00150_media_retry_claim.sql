-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.claim_media_retry_job()
RETURNS TABLE(event_id uuid,materialize_token uuid,tenant_id uuid,bulk_id uuid,item_id uuid,asset_version_id uuid,
 variants jsonb,master_artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,
 media_type text,width integer,height integer,output_role text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.media_retry_jobs%ROWTYPE;token uuid:=gen_random_uuid();a photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO j FROM photo.media_retry_jobs x WHERE x.status='queued' OR (x.status='materializing' AND x.materialize_lease_until<=clock_timestamp())
 ORDER BY x.created_at,x.event_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 IF NOT EXISTS(SELECT FROM photo.bulk_operations b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  WHERE b.tenant_id=j.tenant_id AND b.id=j.bulk_operation_id AND b.action='retry_media' AND NOT b.cancel_requested
  AND b.status IN('completed','completed_with_errors') AND NOT r.cancel_requested)
 OR NOT EXISTS(SELECT FROM photo.collection_items i WHERE i.tenant_id=j.tenant_id AND i.id=j.item_id AND i.asset_version_id=j.asset_version_id) THEN
  UPDATE photo.media_retry_jobs AS target SET status='cancelled',error_code='SOURCE_CHANGED',materialize_token=NULL,materialize_lease_until=NULL WHERE target.event_id=j.event_id;RETURN;
 END IF;
 SELECT ar.* INTO a FROM photo.asset_versions v JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id
 WHERE v.tenant_id=j.tenant_id AND v.id=j.asset_version_id AND v.review_state='accepted' AND ar.state='ready'
 AND ar.object_version IS NOT NULL AND ar.object_version NOT IN('','null') AND ar.bytes>0
 AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp());
 IF NOT FOUND OR a.output_role IS DISTINCT FROM 'working_image' OR a.media_type<>'application/json' THEN
  UPDATE photo.media_retry_jobs AS target SET status='failed',error_code='MASTER_UNAVAILABLE',materialize_token=NULL,materialize_lease_until=NULL WHERE target.event_id=j.event_id;RETURN;
 END IF;
 UPDATE photo.media_retry_jobs AS target SET status='materializing',materialize_token=token,materialize_lease_until=clock_timestamp()+interval '60 seconds',error_code=NULL WHERE target.event_id=j.event_id;
 RETURN QUERY SELECT j.event_id,token,j.tenant_id,j.bulk_operation_id,j.item_id,j.asset_version_id,j.variants,
  a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,a.width,a.height,a.output_role;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_media_retry_job() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_media_retry_job() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_media_retry_job() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.renew_media_retry_job(p_event uuid,p_token uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 UPDATE photo.media_retry_jobs SET materialize_lease_until=clock_timestamp()+interval '60 seconds'
 WHERE event_id=p_event AND status='materializing' AND materialize_token=p_token AND materialize_lease_until>clock_timestamp()
 RETURNING true
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_media_retry_job(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_media_retry_job(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_media_retry_job(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.release_media_retry_job(p_event uuid,p_token uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 UPDATE photo.media_retry_jobs SET status='queued',materialize_token=NULL,materialize_lease_until=NULL
 WHERE event_id=p_event AND status='materializing' AND materialize_token=p_token RETURNING true
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_media_retry_job(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_media_retry_job(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_media_retry_job(uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
