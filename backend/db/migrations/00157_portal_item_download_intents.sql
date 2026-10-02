-- +goose Up
-- A single-item intent pins the published membership, resolved preset and ready
-- export. The bearer is still stored only as a keyed hash.
ALTER TABLE photo.download_intents ADD COLUMN intent_kind text NOT NULL DEFAULT 'volume';
ALTER TABLE photo.download_intents ADD COLUMN item_id uuid;
ALTER TABLE photo.download_intents ADD COLUMN preset_id uuid;
ALTER TABLE photo.download_intents ADD CONSTRAINT download_intents_kind CHECK(
 (intent_kind='volume' AND item_id IS NULL AND preset_id IS NULL) OR
 (intent_kind='item' AND item_id IS NOT NULL AND preset_id IS NOT NULL));
ALTER TABLE photo.download_intents ADD FOREIGN KEY(tenant_id,item_id) REFERENCES photo.collection_items(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.download_intents ADD FOREIGN KEY(tenant_id,preset_id) REFERENCES photo.export_presets(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

-- +goose StatementBegin
CREATE FUNCTION photo.create_portal_item_download_intent(p_session_hash bytea,p_csrf_hash bytea,p_key text,p_request_hash text,p_item uuid,p_preset uuid,p_intent uuid,p_token_hash bytea,p_expires timestamptz)
RETURNS TABLE(outcome text,intent_id uuid,expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ident record;l photo.share_links%ROWTYPE;idem photo.idempotency_records%ROWTYPE;existing photo.download_intents%ROWTYPE;
 pr photo.export_presets%ROWTYPE;i photo.collection_items%ROWTYPE;v photo.asset_versions%ROWTYPE;e photo.exports%ROWTYPE;a photo.artifacts%ROWTYPE;until_time timestamptz;
BEGIN
 IF octet_length(p_session_hash)<>32 OR octet_length(p_csrf_hash)<>32 OR p_key !~ '^[!-~]{1,128}$' OR p_request_hash !~ '^[0-9a-f]{64}$'
 OR p_item IS NULL OR p_preset IS NULL OR p_intent IS NULL OR octet_length(p_token_hash)<>32 OR p_expires IS NULL OR p_expires<=clock_timestamp() OR p_expires>clock_timestamp()+interval '60 seconds' THEN
  RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::timestamptz;RETURN;
 END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,p_csrf_hash,'download');
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::timestamptz;RETURN;END IF;
 SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=ident.tenant_id AND x.id=ident.share_id FOR SHARE;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(ident.tenant_id,'guest',ident.session_id,'Portal.Item.Download.Intent',p_key,p_request_hash,clock_timestamp()+interval '30 days')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records x WHERE x.tenant_id=ident.tenant_id AND x.principal_type='guest' AND x.principal_id=ident.session_id AND x.route='Portal.Item.Download.Intent' AND x.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::timestamptz;RETURN;END IF;
 IF idem.response_status IS NOT NULL THEN
  SELECT * INTO existing FROM photo.download_intents x WHERE x.tenant_id=ident.tenant_id AND x.id=idem.resource_id AND x.principal_id=ident.session_id AND x.intent_kind='item';
  IF NOT FOUND THEN RAISE EXCEPTION 'ITEM_DOWNLOAD_INTENT_IDEMPOTENCY' USING ERRCODE='23514';END IF;
  RETURN QUERY SELECT 'replayed',existing.id,existing.expires_at;RETURN;
 END IF;
 SELECT * INTO pr FROM photo.export_presets x WHERE x.tenant_id=ident.tenant_id AND x.id=p_preset AND x.archived_at IS NULL FOR SHARE;
 IF NOT FOUND OR jsonb_typeof(l.allowed_presets)<>'array' OR NOT EXISTS(SELECT FROM jsonb_array_elements(l.allowed_presets) x WHERE
  (jsonb_typeof(x)='string' AND trim(both '"' from x::text)=p_preset::text) OR (jsonb_typeof(x)='object' AND x->>'id'=p_preset::text)) THEN
  RETURN QUERY SELECT 'preset_invalid',NULL::uuid,NULL::timestamptz;RETURN;
 END IF;
 SELECT x.* INTO i FROM photo.collection_items x JOIN photo.collections c ON c.tenant_id=x.tenant_id AND c.id=x.collection_id AND c.id=ident.collection_id AND c.state='published' AND c.deleted_at IS NULL
 JOIN photo.projects p ON p.tenant_id=c.tenant_id AND p.id=c.project_id AND p.download_allowed
 JOIN photo.publications pub ON pub.tenant_id=x.tenant_id AND pub.item_id=x.id AND pub.asset_version_id=x.asset_version_id AND pub.state='published'
 WHERE x.tenant_id=ident.tenant_id AND x.id=p_item AND x.visibility='visible' AND x.moderation_state='accepted' FOR SHARE OF x;
 IF NOT FOUND THEN RETURN QUERY SELECT 'revoked',NULL::uuid,NULL::timestamptz;RETURN;END IF;
 SELECT x.* INTO v FROM photo.asset_versions x JOIN photo.assets source ON source.tenant_id=x.tenant_id AND source.id=x.asset_id AND source.upload_state='verified' AND source.deleted_at IS NULL
 WHERE x.tenant_id=i.tenant_id AND x.id=i.asset_version_id AND x.asset_id=i.asset_id AND x.review_state='accepted' FOR SHARE OF x;
 IF NOT FOUND THEN RETURN QUERY SELECT 'revoked',NULL::uuid,NULL::timestamptz;RETURN;END IF;
 SELECT x.* INTO e FROM photo.exports x WHERE x.tenant_id=v.tenant_id AND x.asset_version_id=v.id AND x.preset_hash=pr.config_hash AND x.status='ready' AND x.artifact_id IS NOT NULL FOR SHARE;
 SELECT x.* INTO a FROM photo.artifacts x WHERE x.tenant_id=e.tenant_id AND x.id=e.artifact_id AND x.state='ready' AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND x.object_version IS NOT NULL AND x.object_version NOT IN ('','null') FOR SHARE;
 IF e.id IS NULL OR a.id IS NULL THEN RETURN QUERY SELECT 'not_ready',NULL::uuid,NULL::timestamptz;RETURN;END IF;
 until_time:=least(p_expires,coalesce(l.expires_at,p_expires));
 INSERT INTO photo.download_intents(id,tenant_id,principal_type,principal_id,share_id,artifact_id,policy_version,token_hash,expires_at,intent_kind,item_id,preset_id)
 VALUES(p_intent,ident.tenant_id,'guest',ident.session_id,ident.share_id,a.id,ident.link_version,p_token_hash,until_time,'item',i.id,pr.id);
 UPDATE photo.idempotency_records SET response_status=201,response_body=jsonb_build_object('intent_id',p_intent,'expires_at',until_time),resource_id=p_intent WHERE tenant_id=ident.tenant_id AND id=idem.id;
 RETURN QUERY SELECT 'created',p_intent,until_time;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_portal_item_download_intent(bytea,bytea,text,text,uuid,uuid,uuid,bytea,timestamptz) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_portal_item_download_intent(bytea,bytea,text,text,uuid,uuid,uuid,bytea,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_portal_item_download_intent(bytea,bytea,text,text,uuid,uuid,uuid,bytea,timestamptz) TO photo_portal;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.redeem_download_intent(p_token_hash bytea,p_bucket text)
RETURNS TABLE(outcome text,object_key text,object_version text,sha256 text,bytes bigint,media_type text,filename text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE i photo.download_intents%ROWTYPE;s photo.share_sessions%ROWTYPE;l photo.share_links%ROWTYPE;a photo.artifacts%ROWTYPE;v photo.download_volumes%ROWTYPE;j photo.download_jobs%ROWTYPE;c photo.collections%ROWTYPE;p photo.projects%ROWTYPE;m photo.memberships%ROWTYPE;ci photo.collection_items%ROWTYPE;av photo.asset_versions%ROWTYPE;pr photo.export_presets%ROWTYPE;e photo.exports%ROWTYPE;ext text;
BEGIN
 IF octet_length(p_token_hash)<>32 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN QUERY SELECT 'invalid',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 SELECT * INTO i FROM photo.download_intents x WHERE x.token_hash=p_token_hash FOR UPDATE;
 IF NOT FOUND OR i.consumed_at IS NOT NULL OR i.expires_at<=clock_timestamp() OR i.principal_type NOT IN ('guest','user') THEN RETURN QUERY SELECT 'invalid',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 IF i.intent_kind='item' THEN
  SELECT * INTO s FROM photo.share_sessions x WHERE x.tenant_id=i.tenant_id AND x.id=i.principal_id FOR SHARE;
  SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=i.tenant_id AND x.id=i.share_id FOR SHARE;
  IF i.principal_type<>'guest' OR s.id IS NULL OR l.id IS NULL OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp() OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp()) OR s.link_version<>i.policy_version OR l.policy_version<>i.policy_version OR l.permissions->'download'<>'true'::jsonb OR jsonb_typeof(l.allowed_presets)<>'array' OR NOT EXISTS(SELECT FROM jsonb_array_elements(l.allowed_presets) x WHERE (jsonb_typeof(x)='string' AND trim(both '"' from x::text)=i.preset_id::text) OR (jsonb_typeof(x)='object' AND x->>'id'=i.preset_id::text)) THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
  SELECT x.* INTO ci FROM photo.collection_items x JOIN photo.collections col ON col.tenant_id=x.tenant_id AND col.id=x.collection_id AND col.id=s.collection_id AND col.state='published' AND col.deleted_at IS NULL JOIN photo.projects project ON project.tenant_id=col.tenant_id AND project.id=col.project_id AND project.download_allowed JOIN photo.publications pub ON pub.tenant_id=x.tenant_id AND pub.item_id=x.id AND pub.asset_version_id=x.asset_version_id AND pub.state='published' WHERE x.tenant_id=i.tenant_id AND x.id=i.item_id AND x.visibility='visible' AND x.moderation_state='accepted' FOR SHARE OF x;
  SELECT x.* INTO av FROM photo.asset_versions x JOIN photo.assets source ON source.tenant_id=x.tenant_id AND source.id=x.asset_id AND source.upload_state='verified' AND source.deleted_at IS NULL WHERE x.tenant_id=ci.tenant_id AND x.id=ci.asset_version_id AND x.asset_id=ci.asset_id AND x.review_state='accepted' FOR SHARE OF x;
  SELECT * INTO pr FROM photo.export_presets x WHERE x.tenant_id=i.tenant_id AND x.id=i.preset_id AND x.archived_at IS NULL FOR SHARE;
  SELECT x.* INTO e FROM photo.exports x WHERE x.tenant_id=i.tenant_id AND x.asset_version_id=av.id AND x.preset_hash=pr.config_hash AND x.status='ready' AND x.artifact_id=i.artifact_id FOR SHARE;
  SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=i.tenant_id AND x.id=i.artifact_id AND x.bucket=p_bucket AND x.state='ready' AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND x.object_version IS NOT NULL AND x.object_version NOT IN ('','null');
  IF ci.id IS NULL OR av.id IS NULL OR pr.id IS NULL OR e.id IS NULL OR a.id IS NULL THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
  ext:=CASE a.media_type WHEN 'image/jpeg' THEN '.jpg' WHEN 'image/png' THEN '.png' WHEN 'image/tiff' THEN '.tif' WHEN 'image/avif' THEN '.avif' WHEN 'image/webp' THEN '.webp' ELSE '' END;
  UPDATE photo.download_intents SET consumed_at=clock_timestamp() WHERE tenant_id=i.tenant_id AND id=i.id;
  RETURN QUERY SELECT 'ok',a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,'photo-'||left(ci.id::text,8)||ext;RETURN;
 END IF;
 SELECT volume.* INTO v FROM photo.download_volumes volume WHERE volume.tenant_id=i.tenant_id AND volume.artifact_id=i.artifact_id AND volume.status='ready';
 SELECT job.* INTO j FROM photo.download_jobs job WHERE job.tenant_id=i.tenant_id AND job.id=v.download_job_id AND job.principal_type=i.principal_type AND job.principal_id=i.principal_id AND job.status='ready' AND NOT job.cancel_requested AND job.expires_at>clock_timestamp();
 IF i.principal_type='guest' THEN
  SELECT * INTO s FROM photo.share_sessions x WHERE x.tenant_id=i.tenant_id AND x.id=i.principal_id FOR SHARE;SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=i.tenant_id AND x.id=i.share_id FOR SHARE;
  IF s.id IS NULL OR l.id IS NULL OR j.share_id<>i.share_id OR j.policy_version<>i.policy_version OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp() OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp()) OR s.link_version<>i.policy_version OR l.policy_version<>i.policy_version OR l.permissions->'download'<>'true'::jsonb THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
  IF EXISTS(SELECT FROM photo.snapshot_items si LEFT JOIN photo.collection_items x ON x.tenant_id=si.tenant_id AND x.collection_id=j.collection_id AND x.id=si.item_id AND x.asset_version_id=si.asset_version_id AND x.visibility='visible' AND x.moderation_state='accepted' LEFT JOIN photo.publications pub ON pub.tenant_id=si.tenant_id AND pub.item_id=si.item_id AND pub.asset_version_id=si.asset_version_id AND pub.state='published' WHERE si.tenant_id=j.tenant_id AND si.snapshot_id=j.snapshot_id AND (x.id IS NULL OR pub.id IS NULL)) THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 ELSE
  SELECT x.* INTO m FROM photo.memberships x JOIN photo.users u ON u.id=x.user_id AND u.status='active' JOIN photo.tenants t ON t.id=x.tenant_id AND t.status='active' WHERE x.tenant_id=i.tenant_id AND x.user_id=i.principal_id AND x.status='active' FOR SHARE OF x;SELECT * INTO c FROM photo.collections x WHERE x.tenant_id=i.tenant_id AND x.id=j.collection_id AND x.deleted_at IS NULL;SELECT * INTO p FROM photo.projects x WHERE x.tenant_id=i.tenant_id AND x.id=c.project_id AND x.archived_at IS NULL AND x.download_allowed;
  IF m.id IS NULL OR c.id IS NULL OR p.id IS NULL OR (p.visibility='restricted' AND m.role NOT IN ('owner','admin') AND NOT EXISTS(SELECT FROM photo.project_acl x WHERE x.tenant_id=i.tenant_id AND x.project_id=p.id AND x.user_id=i.principal_id AND x.role IN ('viewer','editor'))) THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
  IF EXISTS(SELECT FROM photo.snapshot_items si LEFT JOIN photo.collection_items x ON x.tenant_id=si.tenant_id AND x.collection_id=j.collection_id AND x.id=si.item_id AND x.asset_version_id=si.asset_version_id WHERE si.tenant_id=j.tenant_id AND si.snapshot_id=j.snapshot_id AND x.id IS NULL) THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
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
