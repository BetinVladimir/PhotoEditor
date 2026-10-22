-- +goose Up
-- Completed receipts and media jobs retain their original item UUID after a
-- membership is removed. They are historical evidence, not live membership.
-- Current membership is proven on NEW insertion and again by existing media
-- claim / guest redemption / post-sign authorization paths. No receipt, job,
-- asset, version, quota, object or task is deleted or finalized here.
-- +goose StatementBegin
DO $historical_item_fks$
DECLARE relation regclass;target text;matches integer;
BEGIN
 FOREACH relation IN ARRAY ARRAY['photo.media_retry_jobs'::regclass,'photo.download_intents'::regclass] LOOP
  SELECT count(*),min(c.conname::text) INTO matches,target FROM pg_constraint c
  WHERE c.conrelid=relation AND c.confrelid='photo.collection_items'::regclass AND c.contype='f'
  AND c.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=relation AND attname='tenant_id'),
   (SELECT attnum FROM pg_attribute WHERE attrelid=relation AND attname='item_id')]::smallint[];
  IF matches<>1 THEN RAISE EXCEPTION 'HISTORICAL_ITEM_FOREIGN_KEY_INSTALLATION_DRIFT';END IF;
  EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I',relation,target);
 END LOOP;
END $historical_item_fks$;
CREATE FUNCTION photo.guard_new_media_retry_item() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.origin<>'bulk_repair' THEN RETURN NEW;END IF;
 PERFORM i.id FROM photo.collection_items i JOIN photo.bulk_operations b
 ON b.tenant_id=i.tenant_id AND b.collection_id=i.collection_id AND b.id=NEW.bulk_operation_id AND b.action='retry_media'
 WHERE i.tenant_id=NEW.tenant_id AND i.id=NEW.item_id AND i.asset_version_id=NEW.asset_version_id FOR SHARE OF i;
 IF NOT FOUND THEN RAISE EXCEPTION 'MEDIA_RETRY_CURRENT_ITEM_REQUIRED' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_new_media_retry_item() OWNER TO photo_fn_owner;
CREATE TRIGGER current_bulk_retry_item BEFORE INSERT ON photo.media_retry_jobs FOR EACH ROW EXECUTE FUNCTION photo.guard_new_media_retry_item();
CREATE TRIGGER historical_item_identity BEFORE UPDATE ON photo.media_retry_jobs FOR EACH ROW
EXECUTE FUNCTION photo.reject_content_update('tenant_id','event_id','item_id','asset_version_id','origin','bulk_operation_id');
CREATE FUNCTION photo.guard_new_download_intent_item() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.intent_kind<>'item' THEN RETURN NEW;END IF;
 -- Only a NEW guest item intent reaches this trigger. Existing accepted
 -- receipt replay does not insert or rebind to mutable current state.
 PERFORM i.id FROM photo.collection_items i
 JOIN photo.collections c ON c.tenant_id=i.tenant_id AND c.id=i.collection_id AND c.state='published' AND c.deleted_at IS NULL
 JOIN photo.projects p ON p.tenant_id=c.tenant_id AND p.id=c.project_id AND p.download_allowed
 JOIN photo.share_sessions s ON s.tenant_id=c.tenant_id AND s.id=NEW.principal_id AND s.collection_id=c.id AND s.share_id=NEW.share_id
 JOIN photo.publications pub ON pub.tenant_id=i.tenant_id AND pub.item_id=i.id AND pub.asset_version_id=i.asset_version_id AND pub.state='published'
 JOIN photo.export_presets preset ON preset.tenant_id=i.tenant_id AND preset.id=NEW.preset_id AND preset.archived_at IS NULL
 JOIN photo.exports e ON e.tenant_id=i.tenant_id AND e.asset_version_id=i.asset_version_id AND e.preset_hash=preset.config_hash AND e.status='ready' AND e.artifact_id=NEW.artifact_id
 WHERE i.tenant_id=NEW.tenant_id AND i.id=NEW.item_id AND i.visibility='visible' AND i.moderation_state='accepted'
 AND NEW.principal_type='guest' FOR SHARE OF i;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOWNLOAD_INTENT_CURRENT_ITEM_REQUIRED' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_new_download_intent_item() OWNER TO photo_fn_owner;
CREATE TRIGGER current_download_intent_item BEFORE INSERT ON photo.download_intents FOR EACH ROW EXECUTE FUNCTION photo.guard_new_download_intent_item();
CREATE TRIGGER historical_item_identity BEFORE UPDATE ON photo.download_intents FOR EACH ROW
EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','item_id','intent_kind');
DO $private_triggers$
DECLARE target oid;entry record;
BEGIN
 FOREACH target IN ARRAY ARRAY['photo.guard_new_media_retry_item()'::regprocedure::oid,'photo.guard_new_download_intent_item()'::regprocedure::oid] LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC',target::regprocedure);
  FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
   WHERE p.oid=target AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
   EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',target::regprocedure,(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
  END LOOP;
 END LOOP;
END $private_triggers$;
-- Delayed first delivery after removal is an acknowledged no-op, not a
-- poison SQL23514. An already accepted job still replays before this NEW gate.
DO $retry_intake_current_item$
DECLARE target oid;old_owner oid;old_acl aclitem[];old_config text[];definition text;needle text;replacement text;
BEGIN
 target:=to_regprocedure('photo.accept_media_retry_command(uuid,uuid,uuid,uuid,jsonb)');
 SELECT p.proowner,p.proacl,p.proconfig,pg_get_functiondef(p.oid) INTO old_owner,old_acl,old_config,definition
 FROM pg_proc p WHERE p.oid=target AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo';
 needle:='INSERT INTO photo.media_retry_jobs(event_id,tenant_id,bulk_operation_id,item_id,asset_version_id,variants)';
 replacement:=$patch$PERFORM i.id FROM photo.collection_items i WHERE i.tenant_id=b.tenant_id AND i.collection_id=b.collection_id
 AND i.id=p_item AND i.asset_version_id=p_version FOR SHARE OF i;
 IF NOT FOUND THEN RETURN false;END IF;
 INSERT INTO photo.media_retry_jobs(event_id,tenant_id,bulk_operation_id,item_id,asset_version_id,variants)$patch$;
 IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1
 OR strpos(definition,'IF FOUND THEN RETURN old.tenant_id=b.tenant_id')=0
 OR strpos(definition,'IF FOUND THEN RETURN old.tenant_id=b.tenant_id')>strpos(definition,needle)
 OR strpos(definition,'photo.outbox o WHERE o.tenant_id=b.tenant_id AND o.event_id=p_event')=0 THEN
 RAISE EXCEPTION 'MEDIA_RETRY_INTAKE_INSTALLATION_DRIFT';END IF;
 EXECUTE replace(definition,needle,replacement);
 IF to_regprocedure('photo.accept_media_retry_command(uuid,uuid,uuid,uuid,jsonb)')::oid IS DISTINCT FROM target
 OR NOT EXISTS(SELECT FROM pg_proc p WHERE p.oid=target AND p.proowner=old_owner AND p.proacl IS NOT DISTINCT FROM old_acl
 AND p.proconfig IS NOT DISTINCT FROM old_config AND p.prosecdef AND strpos(p.prosrc,replacement)>0) THEN
 RAISE EXCEPTION 'MEDIA_RETRY_INTAKE_SECURITY_CHANGED';END IF;
END $retry_intake_current_item$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
