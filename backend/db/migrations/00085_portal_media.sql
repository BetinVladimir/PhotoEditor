-- +goose Up
-- Resolve each requested item/variant against the current publication. Missing
-- or unauthorized items are indistinguishable; no partial link is issued.
-- +goose StatementBegin
CREATE FUNCTION photo.resolve_portal_media(p_session_hash bytea,p_bucket text,p_item_ids uuid[],p_variants text[])
RETURNS TABLE(item_ordinal bigint,variant_ordinal bigint,outcome text,item_id uuid,variant text,object_key text,object_version text,
 sha256 text,bytes bigint,media_type text,width int,height int)
LANGUAGE plpgsql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
DECLARE ident record;c photo.collections%ROWTYPE;
BEGIN
 IF p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 OR cardinality(p_item_ids) NOT BETWEEN 1 AND 100 OR cardinality(p_variants) NOT BETWEEN 1 AND 4
 OR EXISTS(SELECT FROM unnest(p_item_ids) x WHERE x IS NULL) OR EXISTS(SELECT FROM unnest(p_variants) x WHERE x NOT IN ('thumb320','thumb640','preview1600','preview2560')) THEN RETURN;END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,NULL,'view');
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO c FROM photo.collections WHERE tenant_id=ident.tenant_id AND id=ident.collection_id AND state='published' AND deleted_at IS NULL;
 IF NOT FOUND THEN RETURN;END IF;
 RETURN QUERY
 WITH requested AS (
  SELECT i.id,i.ord AS io,v.variant,v.ord AS vo FROM unnest(p_item_ids) WITH ORDINALITY i(id,ord) CROSS JOIN unnest(p_variants) WITH ORDINALITY v(variant,ord)
 ),eligible AS (
  SELECT r.*,ci.asset_version_id FROM requested r LEFT JOIN photo.collection_items ci ON ci.tenant_id=c.tenant_id AND ci.collection_id=c.id AND ci.id=r.id
   AND ci.visibility='visible' AND ci.moderation_state='accepted'
   AND EXISTS(SELECT FROM photo.publications p WHERE p.tenant_id=ci.tenant_id AND p.item_id=ci.id AND p.asset_version_id=ci.asset_version_id AND p.state='published')
   AND EXISTS(SELECT FROM photo.asset_versions av JOIN photo.assets a0 ON a0.tenant_id=av.tenant_id AND a0.id=av.asset_id AND a0.deleted_at IS NULL AND a0.upload_state='verified'
    WHERE av.tenant_id=ci.tenant_id AND av.id=ci.asset_version_id AND av.asset_id=ci.asset_id AND av.review_state='accepted')
 )
 SELECT e.io,e.vo,CASE WHEN e.asset_version_id IS NULL THEN 'not_found' WHEN a.id IS NULL THEN 'not_ready' ELSE 'ok' END,
  e.id,e.variant,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,a.width,a.height
 FROM eligible e LEFT JOIN LATERAL (
  SELECT ar.* FROM photo.derivatives d JOIN photo.artifacts ar ON ar.tenant_id=d.tenant_id AND ar.id=d.artifact_id
  WHERE d.tenant_id=c.tenant_id AND d.asset_version_id=e.asset_version_id AND d.variant=e.variant AND d.status='ready'
   AND ar.bucket=p_bucket AND ar.state='ready' AND ar.object_version IS NOT NULL AND ar.object_version NOT IN ('','null')
   AND ar.bytes>0 AND ar.width>0 AND ar.height>0 AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
  ORDER BY d.created_at DESC,d.id DESC LIMIT 1
 ) a ON true ORDER BY e.io,e.vo;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.resolve_portal_media(bytea,text,uuid[],text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.resolve_portal_media(bytea,text,uuid[],text[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.resolve_portal_media(bytea,text,uuid[],text[]) TO photo_portal;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
