-- +goose Up
-- Repeat the item/volume authorization used at redemption after URL signing.
-- The artifact coordinate check from 02131 remains in the same locked read.
-- +goose StatementBegin
DO $$
DECLARE definition text;
BEGIN
 SELECT pg_get_functiondef('photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)'::regprocedure)
 INTO definition;
 IF definition IS NULL
  OR position('a.object_key IS NOT DISTINCT FROM p_object_key' IN definition)=0
  OR position('v30_delivery_download_target' IN definition)=0
  OR position('photo.share_sessions' IN definition)>0 THEN
  RAISE EXCEPTION 'DOWNLOAD_POSTSIGN_POLICY_SOURCE_SHAPE';
 END IF;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_delivery_download_intent_current(
 p_token_hash bytea,p_bucket text,p_object_key text,p_object_version text,
 p_sha text,p_bytes bigint,p_media_type text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE i photo.download_intents%ROWTYPE;target record;found_target boolean;
 a photo.artifacts%ROWTYPE;previous_tenant text;
 s photo.share_sessions%ROWTYPE;l photo.share_links%ROWTYPE;
 v photo.download_volumes%ROWTYPE;j photo.download_jobs%ROWTYPE;
 c photo.collections%ROWTYPE;p photo.projects%ROWTYPE;
 m photo.memberships%ROWTYPE;ci photo.collection_items%ROWTYPE;
 av photo.asset_versions%ROWTYPE;pr photo.export_presets%ROWTYPE;
 e photo.exports%ROWTYPE;
BEGIN
 IF octet_length(p_token_hash)<>32 OR p_bucket IS NULL
  OR length(p_bucket) NOT BETWEEN 3 AND 63 OR p_object_key IS NULL
  OR p_object_version IS NULL OR p_sha IS NULL OR p_bytes IS NULL
  OR p_media_type IS NULL THEN RETURN false;END IF;
 SELECT * INTO i FROM photo.download_intents x WHERE x.token_hash=p_token_hash
  AND x.consumed_at IS NOT NULL AND x.expires_at>clock_timestamp() FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF i.intent_kind IN('item','volume') THEN
  IF i.principal_type NOT IN('guest','user') THEN RETURN false;END IF;
  IF i.intent_kind='item' THEN
   IF i.principal_type<>'guest' THEN RETURN false;END IF;
   SELECT * INTO s FROM photo.share_sessions x WHERE x.tenant_id=i.tenant_id
    AND x.id=i.principal_id FOR SHARE;
   SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=i.tenant_id
    AND x.id=i.share_id FOR SHARE;
   IF s.id IS NULL OR l.id IS NULL OR s.revoked_at IS NOT NULL
    OR s.expires_at<=clock_timestamp() OR l.revoked_at IS NOT NULL
    OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp())
    OR s.link_version IS DISTINCT FROM i.policy_version
    OR l.policy_version IS DISTINCT FROM i.policy_version
    OR l.permissions->'download' IS DISTINCT FROM 'true'::jsonb
    OR jsonb_typeof(l.allowed_presets) IS DISTINCT FROM 'array'
    OR NOT EXISTS(SELECT FROM jsonb_array_elements(l.allowed_presets) x
     WHERE (jsonb_typeof(x)='string' AND trim(both '"' from x::text)=i.preset_id::text)
      OR (jsonb_typeof(x)='object' AND x->>'id'=i.preset_id::text))
   THEN RETURN false;END IF;
   SELECT x.* INTO ci FROM photo.collection_items x
    JOIN photo.collections col ON col.tenant_id=x.tenant_id
     AND col.id=x.collection_id AND col.id=s.collection_id
     AND col.state='published' AND col.deleted_at IS NULL
    JOIN photo.projects project ON project.tenant_id=col.tenant_id
     AND project.id=col.project_id AND project.download_allowed
    JOIN photo.publications pub ON pub.tenant_id=x.tenant_id
     AND pub.item_id=x.id AND pub.asset_version_id=x.asset_version_id
     AND pub.state='published'
    WHERE x.tenant_id=i.tenant_id AND x.id=i.item_id
     AND x.visibility='visible' AND x.moderation_state='accepted'
    FOR SHARE OF x;
   SELECT x.* INTO av FROM photo.asset_versions x
    JOIN photo.assets source ON source.tenant_id=x.tenant_id
     AND source.id=x.asset_id AND source.upload_state='verified'
     AND source.deleted_at IS NULL
    WHERE x.tenant_id=ci.tenant_id AND x.id=ci.asset_version_id
     AND x.asset_id=ci.asset_id AND x.review_state='accepted'
    FOR SHARE OF x;
   SELECT * INTO pr FROM photo.export_presets x WHERE x.tenant_id=i.tenant_id
    AND x.id=i.preset_id AND x.archived_at IS NULL FOR SHARE;
   SELECT x.* INTO e FROM photo.exports x WHERE x.tenant_id=i.tenant_id
    AND x.asset_version_id=av.id AND x.preset_hash=pr.config_hash
    AND x.status='ready' AND x.artifact_id=i.artifact_id FOR SHARE;
   IF ci.id IS NULL OR av.id IS NULL OR pr.id IS NULL OR e.id IS NULL
   THEN RETURN false;END IF;
  ELSE
   SELECT * INTO v FROM photo.download_volumes x WHERE x.tenant_id=i.tenant_id
    AND x.artifact_id=i.artifact_id AND x.status='ready' FOR SHARE;
   SELECT * INTO j FROM photo.download_jobs x WHERE x.tenant_id=i.tenant_id
    AND x.id=v.download_job_id AND x.principal_type=i.principal_type
    AND x.principal_id=i.principal_id AND x.status='ready'
    AND NOT x.cancel_requested AND x.expires_at>clock_timestamp() FOR SHARE;
   IF v.id IS NULL OR j.id IS NULL THEN RETURN false;END IF;
   IF i.principal_type='guest' THEN
    SELECT * INTO s FROM photo.share_sessions x WHERE x.tenant_id=i.tenant_id
     AND x.id=i.principal_id FOR SHARE;
    SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=i.tenant_id
     AND x.id=i.share_id FOR SHARE;
    IF s.id IS NULL OR l.id IS NULL OR j.share_id IS DISTINCT FROM i.share_id
     OR j.policy_version IS DISTINCT FROM i.policy_version
     OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp()
     OR l.revoked_at IS NOT NULL
     OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp())
     OR s.link_version IS DISTINCT FROM i.policy_version
     OR l.policy_version IS DISTINCT FROM i.policy_version
     OR l.permissions->'download' IS DISTINCT FROM 'true'::jsonb
     THEN RETURN false;END IF;
    IF EXISTS(SELECT FROM photo.snapshot_items si
     LEFT JOIN photo.collection_items x ON x.tenant_id=si.tenant_id
      AND x.collection_id=j.collection_id AND x.id=si.item_id
      AND x.asset_version_id=si.asset_version_id AND x.visibility='visible'
      AND x.moderation_state='accepted'
     LEFT JOIN photo.publications pub ON pub.tenant_id=si.tenant_id
      AND pub.item_id=si.item_id AND pub.asset_version_id=si.asset_version_id
      AND pub.state='published'
     WHERE si.tenant_id=j.tenant_id AND si.snapshot_id=j.snapshot_id
      AND (x.id IS NULL OR pub.id IS NULL)) THEN RETURN false;END IF;
   ELSE
    SELECT x.* INTO m FROM photo.memberships x
     JOIN photo.users u ON u.id=x.user_id AND u.status='active'
     JOIN photo.tenants t ON t.id=x.tenant_id AND t.status='active'
     WHERE x.tenant_id=i.tenant_id AND x.user_id=i.principal_id
      AND x.status='active' FOR SHARE OF x;
    SELECT * INTO c FROM photo.collections x WHERE x.tenant_id=i.tenant_id
     AND x.id=j.collection_id AND x.deleted_at IS NULL;
    SELECT * INTO p FROM photo.projects x WHERE x.tenant_id=i.tenant_id
     AND x.id=c.project_id AND x.archived_at IS NULL AND x.download_allowed;
    IF m.id IS NULL OR c.id IS NULL OR p.id IS NULL OR
     (p.visibility='restricted' AND m.role NOT IN('owner','admin')
      AND NOT EXISTS(SELECT FROM photo.project_acl x
       WHERE x.tenant_id=i.tenant_id AND x.project_id=p.id
        AND x.user_id=i.principal_id AND x.role IN('viewer','editor')))
     THEN RETURN false;END IF;
    IF EXISTS(SELECT FROM photo.snapshot_items si
     LEFT JOIN photo.collection_items x ON x.tenant_id=si.tenant_id
      AND x.collection_id=j.collection_id AND x.id=si.item_id
      AND x.asset_version_id=si.asset_version_id
     WHERE si.tenant_id=j.tenant_id AND si.snapshot_id=j.snapshot_id
      AND x.id IS NULL) THEN RETURN false;END IF;
   END IF;
  END IF;
  SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=i.tenant_id
   AND x.id=i.artifact_id AND x.bucket=p_bucket AND x.state='ready'
   AND (i.intent_kind='item' AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp())
    OR i.intent_kind='volume' AND x.expires_at>clock_timestamp())
   AND x.object_version IS NOT NULL AND x.object_version NOT IN ('','null')
   FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
  RETURN a.object_key IS NOT DISTINCT FROM p_object_key
   AND a.object_version IS NOT DISTINCT FROM p_object_version
   AND a.sha256 IS NOT DISTINCT FROM p_sha
   AND a.bytes IS NOT DISTINCT FROM p_bytes
   AND a.media_type IS NOT DISTINCT FROM p_media_type;
 END IF;
 IF i.intent_kind<>'delivery' THEN RETURN false;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',i.tenant_id::text,true);
 SELECT * INTO target FROM photo.v30_delivery_download_target(i.tenant_id,
  i.principal_id,i.delivery_session_id,i.delivery_package_id,i.delivery_kind,
  i.delivery_ordinal,i.delivery_preset_id,p_bucket);
 found_target:=FOUND;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 IF NOT found_target THEN RETURN false;END IF;
 RETURN target.artifact_id IS NOT DISTINCT FROM i.artifact_id
  AND target.sha256 IS NOT DISTINCT FROM i.delivery_sha
  AND target.object_version IS NOT DISTINCT FROM i.delivery_object_version
  AND target.bucket IS NOT DISTINCT FROM p_bucket
  AND target.object_key IS NOT DISTINCT FROM p_object_key
  AND target.object_version IS NOT DISTINCT FROM p_object_version
  AND target.sha256 IS NOT DISTINCT FROM p_sha
  AND target.bytes IS NOT DISTINCT FROM p_bytes
  AND target.media_type IS NOT DISTINCT FROM p_media_type;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)
 TO photo_api,photo_portal;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
