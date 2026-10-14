-- +goose Up
-- Reject a legacy item/volume URL if its exact artifact changed between
-- one-use redemption and external URL signing. Delivery retains its stronger
-- package/session/policy recheck from 01882.
-- +goose StatementBegin
DO $$
DECLARE definition text;
BEGIN
 SELECT pg_get_functiondef('photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)'::regprocedure)
 INTO definition;
 IF definition IS NULL OR position('IF i.intent_kind IN(''item'',''volume'') THEN RETURN true;END IF;' IN definition)=0
  OR position('v30_delivery_download_target' IN definition)=0 THEN
  RAISE EXCEPTION 'DOWNLOAD_POSTSIGN_SOURCE_SHAPE';
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
BEGIN
 IF octet_length(p_token_hash)<>32 OR p_bucket IS NULL
  OR length(p_bucket) NOT BETWEEN 3 AND 63 OR p_object_key IS NULL
  OR p_object_version IS NULL OR p_sha IS NULL OR p_bytes IS NULL
  OR p_media_type IS NULL THEN RETURN false;END IF;
 SELECT * INTO i FROM photo.download_intents x WHERE x.token_hash=p_token_hash
  AND x.consumed_at IS NOT NULL AND x.expires_at>clock_timestamp() FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF i.intent_kind IN('item','volume') THEN
  SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=i.tenant_id
   AND x.id=i.artifact_id AND x.bucket=p_bucket AND x.state='ready'
   AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp())
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
