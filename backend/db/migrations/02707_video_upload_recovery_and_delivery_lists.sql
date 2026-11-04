-- +goose Up
-- 1. Sweeper: an upload whose completion lease ran out (the API or scheduler died between complete_video_upload and
--    finish_video_upload_completion) is returned to 'uploading' so the client can complete it again, instead of waiting
--    for the whole session to expire. Parts stay recorded; the old token is dropped, so a late finisher is refused.
-- 2. Lists for the studio UI: deliveries of a project with their renders and shares, so the UI does not have to
--    remember them in the browser.

-- +goose StatementBegin
CREATE FUNCTION photo.recover_video_upload_completions(p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n integer;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 1000 THEN RAISE EXCEPTION 'LIMIT_INVALID' USING ERRCODE='22023';END IF;
 WITH due AS (SELECT tenant_id,id FROM photo.video_upload_sessions
   WHERE state='completing' AND completion_lease_until<=clock_timestamp() AND expires_at>clock_timestamp()
   ORDER BY completion_lease_until LIMIT p_limit FOR UPDATE SKIP LOCKED)
 UPDATE photo.video_upload_sessions s SET state='uploading',completion_token=NULL,completion_lease_until=NULL,revision=revision+1,updated_at=clock_timestamp()
  FROM due WHERE s.tenant_id=due.tenant_id AND s.id=due.id;
 GET DIAGNOSTICS n=ROW_COUNT;
 RETURN n;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.list_video_deliveries(p_tenant uuid,p_project uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(jsonb_agg(jsonb_build_object('id',c.id,'name',c.name,'state',c.state,
  'items',coalesce((SELECT jsonb_agg(jsonb_build_object('render_id',i.render_id,'title',i.title,'ordinal',i.ordinal) ORDER BY i.ordinal)
    FROM photo.video_collection_items i WHERE i.tenant_id=c.tenant_id AND i.collection_id=c.id),'[]'::jsonb),
  'shares',coalesce((SELECT jsonb_agg(jsonb_build_object('id',l.id,'label',l.label,'download',(l.permissions->>'download')::boolean,
    'expires_at',l.expires_at,'revoked',l.revoked_at IS NOT NULL) ORDER BY l.created_at DESC)
    FROM photo.share_links l WHERE l.tenant_id=c.tenant_id AND l.collection_id=c.id),'[]'::jsonb)) ORDER BY c.created_at DESC),'[]'::jsonb)
 FROM photo.collections c WHERE c.tenant_id=p_tenant AND c.project_id=p_project AND c.content_kind='video' AND c.deleted_at IS NULL
$$;
-- +goose StatementEnd

ALTER FUNCTION photo.recover_video_upload_completions(integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.list_video_deliveries(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.recover_video_upload_completions(integer),photo.list_video_deliveries(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.recover_video_upload_completions(integer) TO photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.list_video_deliveries(uuid,uuid) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
