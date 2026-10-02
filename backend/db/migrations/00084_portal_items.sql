-- +goose Up
-- Only accepted, visible and currently published memberships are enumerable.
-- The projection intentionally omits asset/version/artifact IDs and metadata.
-- +goose StatementBegin
CREATE FUNCTION photo.list_portal_items(p_session_hash bytea,p_section uuid,p_after_position bigint,p_after_id uuid,p_limit int)
RETURNS TABLE(collection_revision bigint,item_id uuid,section_id uuid,"position" bigint,width int,height int,title text,thumbnail_status text,media_variants jsonb)
LANGUAGE plpgsql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
DECLARE ident record;c photo.collections%ROWTYPE;
BEGIN
 IF p_limit NOT BETWEEN 1 AND 101 OR (p_after_position IS NULL)<>(p_after_id IS NULL) THEN RETURN;END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,NULL,'view');
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO c FROM photo.collections WHERE tenant_id=ident.tenant_id AND id=ident.collection_id AND state='published' AND deleted_at IS NULL;
 IF NOT FOUND OR (p_section IS NOT NULL AND NOT EXISTS(SELECT FROM photo.sections s WHERE s.tenant_id=c.tenant_id AND s.collection_id=c.id AND s.id=p_section)) THEN RETURN;END IF;
 RETURN QUERY
 SELECT c.revision,i.id,i.section_id,i.position,
  CASE WHEN (v.metadata->'canvas'->>'width') ~ '^[0-9]{1,9}$' THEN (v.metadata->'canvas'->>'width')::int ELSE NULL::int END,
  CASE WHEN (v.metadata->'canvas'->>'height') ~ '^[0-9]{1,9}$' THEN (v.metadata->'canvas'->>'height')::int ELSE NULL::int END,
  coalesce(nullif(v.metadata->>'title',''),''),
  CASE WHEN EXISTS(SELECT FROM photo.derivatives d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.state='ready' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) WHERE d.tenant_id=i.tenant_id AND d.asset_version_id=i.asset_version_id AND d.variant IN ('thumb320','thumb640') AND d.status='ready') THEN 'ready'
       WHEN EXISTS(SELECT FROM photo.derivatives d WHERE d.tenant_id=i.tenant_id AND d.asset_version_id=i.asset_version_id AND d.variant IN ('thumb320','thumb640') AND d.status IN ('queued','running')) THEN 'pending' ELSE 'missing' END,
  coalesce((SELECT jsonb_agg(jsonb_build_object('variant',x.variant,'status',x.status,'width',x.width,'height',x.height,'media_type',x.media_type) ORDER BY x.variant)
   FROM (SELECT DISTINCT ON (d.variant) d.variant,d.status,a.width,a.height,a.media_type
    FROM photo.derivatives d LEFT JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.state='ready' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    WHERE d.tenant_id=i.tenant_id AND d.asset_version_id=i.asset_version_id AND d.variant IN ('thumb320','thumb640','preview1600','preview2560')
    ORDER BY d.variant,d.created_at DESC,d.id DESC) x),'[]'::jsonb)
 FROM photo.collection_items i
 JOIN photo.publications p ON p.tenant_id=i.tenant_id AND p.item_id=i.id AND p.asset_version_id=i.asset_version_id AND p.state='published'
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id AND v.review_state='accepted'
 JOIN photo.assets a0 ON a0.tenant_id=i.tenant_id AND a0.id=i.asset_id AND a0.deleted_at IS NULL AND a0.upload_state='verified'
 WHERE i.tenant_id=c.tenant_id AND i.collection_id=c.id AND i.visibility='visible' AND i.moderation_state='accepted'
 AND (p_section IS NULL OR i.section_id=p_section) AND (p_after_position IS NULL OR (i.position,i.id)>(p_after_position,p_after_id))
 ORDER BY i.position,i.id LIMIT p_limit;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.list_portal_items(bytea,uuid,bigint,uuid,int) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.list_portal_items(bytea,uuid,bigint,uuid,int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.list_portal_items(bytea,uuid,bigint,uuid,int) TO photo_portal;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
