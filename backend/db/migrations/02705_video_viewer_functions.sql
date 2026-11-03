-- +goose Up
-- Viewer side of a video delivery. The viewer authenticates with the ordinary portal share session
-- (photo.exchange_share_session / authorize_portal_session); these functions give photo_portal read access to the
-- published video delivery of that session's collection and to the files of its renders, nothing else.

-- +goose StatementBegin
CREATE FUNCTION photo.get_video_delivery_view(p_tenant uuid,p_collection uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('name',c.name,'items',coalesce((SELECT jsonb_agg(jsonb_build_object('render_id',i.render_id,'title',i.title,'ordinal',i.ordinal,
   'duration_ticks',r.duration_ticks,'width',r.width,'height',r.height,'bytes',r.output_bytes) ORDER BY i.ordinal)
   FROM photo.video_collection_items i JOIN photo.video_renders r ON r.tenant_id=i.tenant_id AND r.id=i.render_id AND r.state='ready' AND r.deleted_at IS NULL
   WHERE i.tenant_id=c.tenant_id AND i.collection_id=c.id),'[]'::jsonb))
 FROM photo.collections c WHERE c.tenant_id=p_tenant AND c.id=p_collection AND c.content_kind='video' AND c.state='published' AND c.deleted_at IS NULL
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.get_video_delivery_object(p_tenant uuid,p_collection uuid,p_render uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('bucket',a.bucket,'object_key',a.object_key,'media_type',a.media_type,'bytes',a.bytes)
 FROM photo.collections c JOIN photo.video_collection_items i ON i.tenant_id=c.tenant_id AND i.collection_id=c.id AND i.render_id=p_render
  JOIN photo.video_renders r ON r.tenant_id=i.tenant_id AND r.id=i.render_id AND r.state='ready' AND r.deleted_at IS NULL
  JOIN photo.artifacts a ON a.tenant_id=r.tenant_id AND a.id=r.output_artifact_id AND a.state='ready'
 WHERE c.tenant_id=p_tenant AND c.id=p_collection AND c.content_kind='video' AND c.state='published' AND c.deleted_at IS NULL
$$;
-- +goose StatementEnd

ALTER FUNCTION photo.get_video_delivery_view(uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.get_video_delivery_object(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_video_delivery_view(uuid,uuid),photo.get_video_delivery_object(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.get_video_delivery_view(uuid,uuid),photo.get_video_delivery_object(uuid,uuid,uuid) TO photo_portal;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
