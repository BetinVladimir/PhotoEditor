-- +goose Up
-- The version replacement endpoint intentionally advances a collection item to
-- an accepted direct child. Preserve identity immutability and validate that
-- sole permitted content transition instead of rejecting every replacement.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_collection_item_version() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.asset_version_id IS NOT DISTINCT FROM OLD.asset_version_id THEN RETURN NEW;END IF;
 IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.id IS DISTINCT FROM OLD.id
 OR NEW.asset_id IS DISTINCT FROM OLD.asset_id OR NEW.collection_id IS DISTINCT FROM OLD.collection_id THEN
  RAISE EXCEPTION 'COLLECTION_ITEM_IDENTITY_IMMUTABLE' USING ERRCODE='23514';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.collections c
  JOIN photo.assets asset ON asset.tenant_id=c.tenant_id AND asset.project_id=c.project_id AND asset.id=NEW.asset_id
  JOIN photo.asset_versions v ON v.tenant_id=asset.tenant_id AND v.asset_id=asset.id AND v.id=NEW.asset_version_id
  JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
  WHERE c.tenant_id=NEW.tenant_id AND c.id=NEW.collection_id AND c.deleted_at IS NULL AND c.state IN('draft','published')
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
  AND v.parent_asset_version_id=OLD.asset_version_id AND v.review_state='accepted'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'COLLECTION_ITEM_VERSION_NOT_ACCEPTED_CHILD' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_collection_item_version() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_collection_item_version() FROM PUBLIC;
DO $install$
DECLARE definition text;
BEGIN
 SELECT pg_get_triggerdef(t.oid) INTO definition FROM pg_trigger t
 WHERE t.tgrelid='photo.collection_items'::regclass AND t.tgname='immutable_content' AND NOT t.tgisinternal
 AND t.tgfoid='photo.reject_content_update()'::regprocedure AND t.tgnargs=4;
 IF definition IS NULL OR strpos(definition,'''tenant_id'', ''id'', ''asset_id'', ''asset_version_id''')=0 THEN
  RAISE EXCEPTION 'COLLECTION_ITEM_IMMUTABILITY_INSTALLATION_DRIFT';END IF;
 DROP TRIGGER immutable_content ON photo.collection_items;
 CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.collection_items FOR EACH ROW
 EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','asset_id');
 CREATE TRIGGER accepted_version_transition BEFORE UPDATE OF asset_version_id ON photo.collection_items FOR EACH ROW
 EXECUTE FUNCTION photo.guard_collection_item_version();
END $install$;
-- Trigger-only helper: remove default role grants as well as PUBLIC.
DO $acl$ DECLARE entry record;BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.guard_collection_item_version()'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION photo.guard_collection_item_version() FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;

-- Historical publications pin their original immutable version, while an item
-- can advance to a newer accepted version. Keep tenant/version and item FKs;
-- enforce current-version identity when creating a new publication only.
-- Publication is access-policy metadata for a collection membership. Removing
-- that membership removes its dependent publication rows, never its immutable
-- assets/versions/snapshots/objects. History remains immutable while item exists.
CREATE FUNCTION photo.guard_publication_current_item_version() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 -- Fence a concurrent version replacement until this publication commits.
 -- After waiting, PostgreSQL rechecks the exact current-version predicate.
 PERFORM i.id FROM photo.collection_items i WHERE i.tenant_id=NEW.tenant_id
 AND i.id=NEW.item_id AND i.asset_version_id=NEW.asset_version_id FOR SHARE OF i;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'PUBLICATION_CURRENT_ITEM_VERSION_REQUIRED' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_publication_current_item_version() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_publication_current_item_version() FROM PUBLIC;
DO $publication_identity$
DECLARE target text;matches integer;
BEGIN
 SELECT count(*),min(c.conname::text) INTO matches,target FROM pg_constraint c
 WHERE c.conrelid='photo.publications'::regclass AND c.confrelid='photo.collection_items'::regclass AND c.contype='f'
 AND c.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='tenant_id'),
 (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='item_id'),
 (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='asset_version_id')]::smallint[];
 IF matches<>1 THEN RAISE EXCEPTION 'PUBLICATION_VERSION_FOREIGN_KEY_INSTALLATION_DRIFT';END IF;
 EXECUTE format('ALTER TABLE photo.publications DROP CONSTRAINT %I',target);
 ALTER TABLE photo.publications ADD CONSTRAINT publications_item_identity_fk FOREIGN KEY(tenant_id,item_id)
 REFERENCES photo.collection_items(tenant_id,id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED;
 CREATE TRIGGER current_item_version BEFORE INSERT ON photo.publications FOR EACH ROW
 EXECUTE FUNCTION photo.guard_publication_current_item_version();
END $publication_identity$;
DO $acl$ DECLARE entry record;BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.guard_publication_current_item_version()'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION photo.guard_publication_current_item_version() FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;
-- Explicit publication of a new current version creates a new identity.
-- Historical version rows remain immutable and can still be revoked without
-- being silently repointed to the newer version.
DO $publication_version_key$
DECLARE target text;matches integer;
BEGIN
 SELECT count(*),min(c.conname::text) INTO matches,target FROM pg_constraint c
 WHERE c.conrelid='photo.publications'::regclass AND c.contype='u' AND NOT c.condeferrable
 AND c.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='tenant_id'),
 (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='item_id')]::smallint[];
 IF matches<>1 THEN RAISE EXCEPTION 'PUBLICATION_ITEM_UNIQUE_INSTALLATION_DRIFT';END IF;
 EXECUTE format('ALTER TABLE photo.publications DROP CONSTRAINT %I',target);
 ALTER TABLE photo.publications ADD CONSTRAINT publications_item_version_key UNIQUE(tenant_id,item_id,asset_version_id);
END $publication_version_key$;

-- Patch the installed bulk function, including every intervening security and
-- color-label update, rather than reinstalling the historical definition.
DO $bulk_publication_version$
DECLARE target oid;old_owner oid;old_acl aclitem[];old_config text[];definition text;needle text;replacement text;
BEGIN
 target:=to_regprocedure('photo.process_bulk_chunk(uuid,uuid)');
 SELECT p.proowner,p.proacl,p.proconfig,pg_get_functiondef(p.oid) INTO old_owner,old_acl,old_config,definition
 FROM pg_proc p WHERE p.oid=target AND p.prosecdef AND p.prorettype='boolean'::regtype
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo';
 IF definition IS NULL THEN RAISE EXCEPTION 'BULK_PUBLICATION_SECURITY_INSTALLATION_DRIFT';END IF;
 needle:='ON CONFLICT(tenant_id,item_id) DO UPDATE SET state=''published'',published_by=EXCLUDED.published_by,published_at=EXCLUDED.published_at,policy_version=photo.publications.policy_version+1;';
 replacement:='ON CONFLICT(tenant_id,item_id,asset_version_id) DO UPDATE SET state=''published'',published_by=EXCLUDED.published_by,published_at=EXCLUDED.published_at,policy_version=photo.publications.policy_version+1;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1
 OR strpos(definition,'INSERT INTO photo.publications(tenant_id,item_id,asset_version_id,state,published_by,published_at) VALUES(b.tenant_id,item.item_id,item.asset_version_id,''published'',b.created_by,clock_timestamp())')=0
 OR strpos(definition,'WHEN ''publish'' THEN')=0 THEN
  RAISE EXCEPTION 'BULK_PUBLICATION_CONFLICT_INSTALLATION_DRIFT';END IF;
 EXECUTE replace(definition,needle,replacement);
 IF to_regprocedure('photo.process_bulk_chunk(uuid,uuid)')::oid IS DISTINCT FROM target
 OR NOT EXISTS(SELECT FROM pg_proc p WHERE p.oid=target AND p.proowner=old_owner AND p.proacl IS NOT DISTINCT FROM old_acl
 AND p.proconfig IS NOT DISTINCT FROM old_config AND p.prosecdef
 AND strpos(p.prosrc,replacement)>0 AND strpos(p.prosrc,needle)=0) THEN
  RAISE EXCEPTION 'BULK_PUBLICATION_SECURITY_CHANGED';END IF;
END $bulk_publication_version$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
