-- +goose Up
-- Redeem both guest and authenticated-user intents through one opaque bearer.
-- +goose StatementBegin
CREATE FUNCTION photo.redeem_download_intent(p_token_hash bytea,p_bucket text)
RETURNS TABLE(outcome text,object_key text,object_version text,sha256 text,bytes bigint,media_type text,filename text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE i photo.download_intents%ROWTYPE;s photo.share_sessions%ROWTYPE;l photo.share_links%ROWTYPE;a photo.artifacts%ROWTYPE;v photo.download_volumes%ROWTYPE;j photo.download_jobs%ROWTYPE;c photo.collections%ROWTYPE;p photo.projects%ROWTYPE;m photo.memberships%ROWTYPE;
BEGIN
 IF octet_length(p_token_hash)<>32 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN QUERY SELECT 'invalid',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 SELECT * INTO i FROM photo.download_intents x WHERE x.token_hash=p_token_hash FOR UPDATE;
 IF NOT FOUND OR i.consumed_at IS NOT NULL OR i.expires_at<=clock_timestamp() OR i.principal_type NOT IN ('guest','user') THEN RETURN QUERY SELECT 'invalid',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 SELECT volume.* INTO v FROM photo.download_volumes volume WHERE volume.tenant_id=i.tenant_id AND volume.artifact_id=i.artifact_id AND volume.status='ready';
 SELECT job.* INTO j FROM photo.download_jobs job WHERE job.tenant_id=i.tenant_id AND job.id=v.download_job_id AND job.principal_type=i.principal_type AND job.principal_id=i.principal_id AND job.status='ready' AND NOT job.cancel_requested AND job.expires_at>clock_timestamp();
 IF i.principal_type='guest' THEN
  SELECT * INTO s FROM photo.share_sessions x WHERE x.tenant_id=i.tenant_id AND x.id=i.principal_id FOR SHARE;
  SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=i.tenant_id AND x.id=i.share_id FOR SHARE;
  IF s.id IS NULL OR l.id IS NULL OR j.share_id<>i.share_id OR j.policy_version<>i.policy_version OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp() OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp()) OR s.link_version<>i.policy_version OR l.policy_version<>i.policy_version OR l.permissions->'download'<>'true'::jsonb THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
  IF EXISTS(SELECT FROM photo.snapshot_items si LEFT JOIN photo.collection_items ci ON ci.tenant_id=si.tenant_id AND ci.collection_id=j.collection_id AND ci.id=si.item_id AND ci.asset_version_id=si.asset_version_id AND ci.visibility='visible' AND ci.moderation_state='accepted' LEFT JOIN photo.publications pub ON pub.tenant_id=si.tenant_id AND pub.item_id=si.item_id AND pub.asset_version_id=si.asset_version_id AND pub.state='published' WHERE si.tenant_id=j.tenant_id AND si.snapshot_id=j.snapshot_id AND (ci.id IS NULL OR pub.id IS NULL)) THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 ELSE
  SELECT x.* INTO m FROM photo.memberships x JOIN photo.users u ON u.id=x.user_id AND u.status='active' JOIN photo.tenants t ON t.id=x.tenant_id AND t.status='active' WHERE x.tenant_id=i.tenant_id AND x.user_id=i.principal_id AND x.status='active' FOR SHARE OF x;
  SELECT * INTO c FROM photo.collections x WHERE x.tenant_id=i.tenant_id AND x.id=j.collection_id AND x.deleted_at IS NULL;
  SELECT * INTO p FROM photo.projects x WHERE x.tenant_id=i.tenant_id AND x.id=c.project_id AND x.archived_at IS NULL AND x.download_allowed;
  IF m.id IS NULL OR c.id IS NULL OR p.id IS NULL OR (p.visibility='restricted' AND m.role NOT IN ('owner','admin') AND NOT EXISTS(SELECT FROM photo.project_acl x WHERE x.tenant_id=i.tenant_id AND x.project_id=p.id AND x.user_id=i.principal_id AND x.role IN ('viewer','editor'))) THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
  IF EXISTS(SELECT FROM photo.snapshot_items si LEFT JOIN photo.collection_items ci ON ci.tenant_id=si.tenant_id AND ci.collection_id=j.collection_id AND ci.id=si.item_id AND ci.asset_version_id=si.asset_version_id WHERE si.tenant_id=j.tenant_id AND si.snapshot_id=j.snapshot_id AND ci.id IS NULL) THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 END IF;
 SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=i.tenant_id AND x.id=i.artifact_id AND x.bucket=p_bucket AND x.state='ready' AND x.expires_at>clock_timestamp() AND x.object_version IS NOT NULL AND x.object_version NOT IN ('','null');
 IF v.id IS NULL OR j.id IS NULL OR a.id IS NULL THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 UPDATE photo.download_intents SET consumed_at=clock_timestamp() WHERE tenant_id=i.tenant_id AND id=i.id;
 RETURN QUERY SELECT 'ok',a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,CASE WHEN a.media_type='application/zip' THEN 'photos-'||lpad(v.volume_no::text,3,'0')||'.zip' ELSE (SELECT entry_name FROM photo.download_job_items x WHERE x.tenant_id=v.tenant_id AND x.download_job_id=v.download_job_id AND x.volume_no=v.volume_no LIMIT 1) END;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.redeem_download_intent(bytea,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.redeem_download_intent(bytea,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.redeem_download_intent(bytea,text) TO photo_api,photo_portal;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
