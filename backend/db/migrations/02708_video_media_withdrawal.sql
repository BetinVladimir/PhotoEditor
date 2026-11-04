-- +goose Up
-- FN-D-13: two-phase removal of a library item. withdraw_video_media makes it unusable (ready -> withdrawing);
-- the maintenance worker lists the objects (list_video_media_deletions), removes them from the object store and only then
-- calls finish_video_media_delete, which settles storage_bytes (source + derivatives, as charged at ingest) and publishes 'deleted'.

-- +goose StatementBegin
CREATE FUNCTION photo.withdraw_video_media(p_tenant uuid,p_media uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE m photo.video_media%ROWTYPE;
BEGIN
 SELECT * INTO m FROM photo.video_media WHERE tenant_id=p_tenant AND id=p_media FOR UPDATE;
 IF NOT FOUND OR m.state='deleted' THEN RAISE EXCEPTION 'VIDEO_MEDIA_NOT_FOUND' USING ERRCODE='P0002';END IF;
 IF m.state='withdrawing' THEN RETURN true;END IF;
 IF m.state<>'ready' THEN RAISE EXCEPTION 'VIDEO_MEDIA_STATE' USING ERRCODE='55000';END IF;
 IF EXISTS(SELECT FROM photo.video_jobs j WHERE j.tenant_id=p_tenant AND j.state IN('queued','running') AND j.media_id=p_media) THEN
  RAISE EXCEPTION 'VIDEO_MEDIA_BUSY' USING ERRCODE='55000';END IF;
 UPDATE photo.video_media SET state='withdrawing',withdrawn_at=clock_timestamp(),revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=p_media;
 RETURN true;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.list_video_media_deletions(p_limit integer) RETURNS jsonb
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 1000 THEN RAISE EXCEPTION 'LIMIT_INVALID' USING ERRCODE='22023';END IF;
 RETURN coalesce((SELECT jsonb_agg(jsonb_build_object('tenant_id',x.tenant_id,'media_id',x.id,'objects',(
   SELECT jsonb_agg(jsonb_build_object('bucket',a.bucket,'object_key',a.object_key))
   FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND a.id IN(x.source_artifact_id,x.probe_artifact_id,x.proxy_artifact_id,x.filmstrip_artifact_id,x.waveform_artifact_id))))
  FROM (SELECT * FROM photo.video_media WHERE state='withdrawing' ORDER BY withdrawn_at LIMIT p_limit) x),'[]'::jsonb);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.finish_video_media_delete(p_tenant uuid,p_media uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE m photo.video_media%ROWTYPE;total bigint;
BEGIN
 PERFORM 1 FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO m FROM photo.video_media WHERE tenant_id=p_tenant AND id=p_media FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF m.state='deleted' THEN RETURN true;END IF;
 IF m.state<>'withdrawing' THEN RETURN false;END IF;
 SELECT coalesce(sum(a.bytes),0) INTO total FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.state<>'deleted'
  AND a.id IN(m.source_artifact_id,m.probe_artifact_id,m.proxy_artifact_id,m.filmstrip_artifact_id,m.waveform_artifact_id);
 UPDATE photo.tenant_usage SET storage_bytes=storage_bytes-total WHERE tenant_id=p_tenant AND storage_bytes>=total;
 IF NOT FOUND THEN RAISE EXCEPTION 'VIDEO_STORAGE_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.artifacts SET state='deleted',gc_lease_until=NULL,gc_storage_settled=true
  WHERE tenant_id=p_tenant AND state<>'deleted' AND id IN(m.source_artifact_id,m.probe_artifact_id,m.proxy_artifact_id,m.filmstrip_artifact_id,m.waveform_artifact_id);
 UPDATE photo.video_media SET state='deleted',revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=p_media;
 RETURN true;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.withdraw_video_media(uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.list_video_media_deletions(integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.finish_video_media_delete(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.withdraw_video_media(uuid,uuid),photo.list_video_media_deletions(integer),photo.finish_video_media_delete(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.withdraw_video_media(uuid,uuid) TO photo_api;
GRANT EXECUTE ON FUNCTION photo.list_video_media_deletions(integer),photo.finish_video_media_delete(uuid,uuid) TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
