-- +goose Up
-- Freeze a guest-visible ordered selection for downloads. The collection row is
-- locked while the digest and items are written, so source_revision is exact.
-- +goose StatementBegin
CREATE FUNCTION photo.create_portal_selection(p_session_hash bytea,p_csrf_hash bytea,p_key text,p_request_hash text,p_snapshot uuid,
 p_collection_revision bigint,p_scope text,p_item_ids uuid[],p_section uuid,p_expires timestamptz)
RETURNS TABLE(outcome text,snapshot_id uuid,digest text,item_count bigint,expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ident record;l photo.share_links%ROWTYPE;s photo.share_sessions%ROWTYPE;c photo.collections%ROWTYPE;idem photo.idempotency_records%ROWTYPE;
 count_value bigint;digest_value text;until_time timestamptz;
BEGIN
 IF octet_length(p_session_hash)<>32 OR octet_length(p_csrf_hash)<>32 OR p_key !~ '^[!-~]{1,128}$' OR p_request_hash !~ '^[0-9a-f]{64}$'
 OR p_snapshot IS NULL OR p_collection_revision NOT BETWEEN 1 AND 9007199254740991 OR p_scope NOT IN ('selected','all','filter')
 OR (p_scope='selected' AND (cardinality(p_item_ids) NOT BETWEEN 1 AND 5000 OR p_section IS NOT NULL))
 OR (p_scope='all' AND (p_item_ids IS NOT NULL OR p_section IS NOT NULL))
 OR (p_scope='filter' AND (p_item_ids IS NOT NULL OR p_section IS NULL)) OR p_expires IS NULL OR p_expires<=clock_timestamp() OR p_expires>clock_timestamp()+interval '24 hours'
 OR (p_item_ids IS NOT NULL AND ((SELECT count(*) FROM unnest(p_item_ids) x WHERE x IS NULL)>0 OR cardinality(p_item_ids)<>(SELECT count(DISTINCT x) FROM unnest(p_item_ids) x))) THEN
  RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;
 END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,p_csrf_hash,'view');
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;END IF;
 SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=ident.tenant_id AND x.id=ident.share_id FOR UPDATE;
 SELECT * INTO s FROM photo.share_sessions x WHERE x.tenant_id=ident.tenant_id AND x.id=ident.session_id FOR UPDATE;
 IF l.id IS NULL OR s.id IS NULL OR s.session_hash<>p_session_hash OR s.csrf_hash<>p_csrf_hash OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp()
 OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp()) OR l.policy_version<>s.link_version OR l.permissions->'view'<>'true'::jsonb THEN
  RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;
 END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(s.tenant_id,'guest',s.id,'Portal.Selection',p_key,p_request_hash,clock_timestamp()+interval '30 days')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records x WHERE x.tenant_id=s.tenant_id AND x.principal_type='guest' AND x.principal_id=s.id AND x.route='Portal.Selection' AND x.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;END IF;
 IF idem.response_status IS NOT NULL THEN
  RETURN QUERY SELECT 'replayed',snap.id,snap.digest,snap.item_count,snap.expires_at FROM photo.snapshots snap WHERE snap.tenant_id=s.tenant_id AND snap.id=idem.resource_id AND snap.principal_type='guest' AND snap.principal_id=s.id;RETURN;
 END IF;
 SELECT * INTO c FROM photo.collections x WHERE x.tenant_id=s.tenant_id AND x.id=s.collection_id AND x.state='published' AND x.deleted_at IS NULL FOR SHARE;
 IF NOT FOUND OR c.revision<>p_collection_revision OR (p_section IS NOT NULL AND NOT EXISTS(SELECT FROM photo.sections sec WHERE sec.tenant_id=c.tenant_id AND sec.collection_id=c.id AND sec.id=p_section)) THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;
 END IF;
 WITH eligible AS (
  SELECT i.id,i.asset_version_id,v.master_artifact_id,v.content_hash,i.position
  FROM photo.collection_items i JOIN photo.publications p ON p.tenant_id=i.tenant_id AND p.item_id=i.id AND p.asset_version_id=i.asset_version_id AND p.state='published'
  JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id AND v.review_state='accepted'
  JOIN photo.assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id AND a.deleted_at IS NULL AND a.upload_state='verified'
  JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id AND ar.sha256=v.content_hash AND ar.state='ready' AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
  WHERE i.tenant_id=c.tenant_id AND i.collection_id=c.id AND i.visibility='visible' AND i.moderation_state='accepted'
  AND (p_scope<>'selected' OR i.id=ANY(p_item_ids)) AND (p_scope<>'filter' OR i.section_id=p_section)
 ) SELECT count(*),encode(digest(string_agg(id::text||':'||asset_version_id::text||':'||content_hash,',' ORDER BY position,id),'sha256'),'hex')
 INTO count_value,digest_value FROM eligible;
 IF count_value<1 OR count_value>5000 OR (p_scope='selected' AND count_value<>cardinality(p_item_ids)) THEN
  RETURN QUERY SELECT CASE WHEN count_value=0 THEN 'empty' ELSE 'invalid' END,NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;
 END IF;
 until_time:=least(p_expires,s.expires_at,coalesce(l.expires_at,p_expires));
 INSERT INTO photo.snapshots(id,tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count,expires_at)
 VALUES(p_snapshot,s.tenant_id,'selection',c.id,c.revision,'guest',s.id,digest_value,count_value,until_time);
 INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload)
 SELECT s.tenant_id,p_snapshot,row_number() OVER(ORDER BY i.position,i.id)-1,i.id,i.asset_version_id,v.master_artifact_id,v.content_hash,jsonb_build_object('position',i.position)
 FROM photo.collection_items i JOIN photo.publications p ON p.tenant_id=i.tenant_id AND p.item_id=i.id AND p.asset_version_id=i.asset_version_id AND p.state='published'
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id AND v.review_state='accepted'
 JOIN photo.assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id AND a.deleted_at IS NULL AND a.upload_state='verified'
 JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id AND ar.sha256=v.content_hash AND ar.state='ready' AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
 WHERE i.tenant_id=c.tenant_id AND i.collection_id=c.id AND i.visibility='visible' AND i.moderation_state='accepted'
 AND (p_scope<>'selected' OR i.id=ANY(p_item_ids)) AND (p_scope<>'filter' OR i.section_id=p_section) ORDER BY i.position,i.id;
 UPDATE photo.idempotency_records SET response_status=201,response_body=jsonb_build_object('id',p_snapshot,'digest',digest_value,'item_count',count_value,'expires_at',until_time),resource_id=p_snapshot WHERE tenant_id=s.tenant_id AND id=idem.id;
 RETURN QUERY SELECT 'created',p_snapshot,digest_value,count_value,until_time;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_portal_selection(bytea,bytea,text,text,uuid,bigint,text,uuid[],uuid,timestamptz) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_portal_selection(bytea,bytea,text,text,uuid,bigint,text,uuid[],uuid,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_portal_selection(bytea,bytea,text,text,uuid,bigint,text,uuid[],uuid,timestamptz) TO photo_portal;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
