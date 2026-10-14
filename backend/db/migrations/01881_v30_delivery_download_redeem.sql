-- +goose Up
-- Extend the existing one-use bearer redemption with a Delivery branch. The
-- exact session, package, slot, artifact SHA and object version are repeated
-- under the intent row lock before consuming the bearer.
-- +goose StatementBegin
DO $$
DECLARE definition text;
 declaration text := 'ext text;';
 item_branch text := ' IF i.intent_kind=''item'' THEN';
 delivery_branch text := $branch$
 IF i.intent_kind='delivery' THEN
  previous_tenant:=current_setting('app.tenant_id',true);
  PERFORM set_config('app.tenant_id',i.tenant_id::text,true);
  SELECT * INTO target FROM photo.v30_delivery_download_target(i.tenant_id,
   i.principal_id,i.delivery_session_id,i.delivery_package_id,i.delivery_kind,
   i.delivery_ordinal,i.delivery_preset_id,p_bucket);
  target_found:=FOUND;
  PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
  IF NOT target_found THEN
   RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;
   RETURN;
  END IF;
  IF target.artifact_id IS DISTINCT FROM i.artifact_id
   OR target.sha256 IS DISTINCT FROM i.delivery_sha
   OR target.object_version IS DISTINCT FROM i.delivery_object_version
   OR target.bucket IS DISTINCT FROM p_bucket THEN
   RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;
   RETURN;
  END IF;
  UPDATE photo.download_intents SET consumed_at=clock_timestamp()
   WHERE tenant_id=i.tenant_id AND id=i.id AND consumed_at IS NULL;
  IF NOT FOUND THEN
   RETURN QUERY SELECT 'invalid',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;
   RETURN;
  END IF;
  ext:=CASE target.media_type WHEN 'application/json' THEN '.json'
   WHEN 'image/jpeg' THEN '.jpg' WHEN 'image/png' THEN '.png'
   WHEN 'image/tiff' THEN '.tif' WHEN 'image/avif' THEN '.avif'
   WHEN 'image/webp' THEN '.webp' ELSE '' END;
  RETURN QUERY SELECT 'ok',target.object_key,target.object_version,target.sha256,
   target.bytes,target.media_type,'delivery-'||left(i.delivery_package_id::text,8)||
   CASE WHEN i.delivery_kind='slot' THEN '-'||lpad(i.delivery_ordinal::text,4,'0') ELSE '-manifest' END||ext;
  RETURN;
 END IF;
 IF i.intent_kind='item' THEN$branch$;
BEGIN
 SELECT pg_get_functiondef('photo.redeem_download_intent(bytea,text)'::regprocedure)
  INTO definition;
 IF definition IS NULL OR position(declaration IN definition)=0
  OR position(item_branch IN definition)=0
  OR position('i.intent_kind=''delivery''' IN definition)>0
  OR position('SELECT * INTO i FROM photo.download_intents x WHERE x.token_hash=p_token_hash FOR UPDATE;' IN definition)=0
  OR position('UPDATE photo.download_intents SET consumed_at=clock_timestamp()' IN definition)=0 THEN
  RAISE EXCEPTION 'DELIVERY_DOWNLOAD_REDEEM_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,declaration,
  'ext text;target record;previous_tenant text;target_found boolean;');
 definition:=replace(definition,item_branch,delivery_branch);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.redeem_download_intent(bytea,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.redeem_download_intent(bytea,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.redeem_download_intent(bytea,text) TO photo_api,photo_portal;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
