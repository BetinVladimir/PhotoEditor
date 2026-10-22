package collection

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// Fail startup against an item-only publication key: both the API and the
// installed bulk publisher require exact immutable version identities.
func RequireCollectionPublicationHistory(ctx context.Context,db artifactjson.RowQuerier)error{
 return RequireCollectionPublicationHistoryForRole(ctx,db,"photo_api")
}
func RequireCollectionPublicationHistoryForRole(ctx context.Context,db artifactjson.RowQuerier,role string)error{
 if role!="photo_api"&&role!="photo_worker"{return errors.New("collection publication role invalid")}
 if err:=ctx.Err();err!=nil{return err};if db==nil{return errors.New("collection publication database required")}
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 var ready bool
 err:=db.QueryRow(ctx,`SELECT current_user=$1
 AND EXISTS(SELECT FROM pg_constraint c WHERE c.conrelid='photo.publications'::regclass AND c.contype='u' AND NOT c.condeferrable
  AND c.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='tenant_id'),(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='item_id'),(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='asset_version_id')]::smallint[])
 AND NOT EXISTS(SELECT FROM pg_constraint c WHERE c.conrelid='photo.publications'::regclass AND c.contype='u'
  AND c.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='tenant_id'),(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='item_id')]::smallint[])
 AND EXISTS(SELECT FROM pg_constraint c WHERE c.conrelid='photo.publications'::regclass AND c.confrelid='photo.collection_items'::regclass AND c.contype='f' AND c.convalidated AND c.confdeltype='c'
  AND c.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='tenant_id'),(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='item_id')]::smallint[])
 AND EXISTS(SELECT FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid WHERE t.tgrelid='photo.collection_items'::regclass
  AND t.tgname='accepted_version_transition' AND t.tgenabled='O' AND t.tgtype=19 AND NOT t.tgisinternal
  AND p.oid=to_regprocedure('photo.guard_collection_item_version()') AND NOT p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'v.parent_asset_version_id=OLD.asset_version_id')>0 AND strpos(p.prosrc,'COLLECTION_ITEM_VERSION_NOT_ACCEPTED_CHILD')>0
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee<>p.proowner AND a.privilege_type='EXECUTE'))
 AND NOT EXISTS(SELECT FROM pg_constraint c WHERE c.conrelid IN('photo.media_retry_jobs'::regclass,'photo.download_intents'::regclass)
  AND c.confrelid='photo.collection_items'::regclass AND c.contype='f')
 AND (SELECT count(*)=2 FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid
  WHERE ((t.tgrelid='photo.media_retry_jobs'::regclass AND t.tgname='current_bulk_retry_item' AND p.oid=to_regprocedure('photo.guard_new_media_retry_item()'))
   OR(t.tgrelid='photo.download_intents'::regclass AND t.tgname='current_download_intent_item' AND p.oid=to_regprocedure('photo.guard_new_download_intent_item()')))
  AND t.tgenabled='O' AND t.tgtype=7 AND NOT t.tgisinternal AND NOT p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'FOR SHARE OF i')>0
  AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo'
  AND ((p.oid=to_regprocedure('photo.guard_new_media_retry_item()') AND strpos(p.prosrc,'i.asset_version_id=NEW.asset_version_id')>0 AND strpos(p.prosrc,'b.action=''retry_media''')>0)
   OR(p.oid=to_regprocedure('photo.guard_new_download_intent_item()') AND strpos(p.prosrc,'pub.asset_version_id=i.asset_version_id')>0 AND strpos(p.prosrc,'s.collection_id=c.id')>0 AND strpos(p.prosrc,'e.artifact_id=NEW.artifact_id')>0))
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee<>p.proowner AND a.privilege_type='EXECUTE'))
 AND (SELECT count(*)=2 FROM pg_trigger t WHERE t.tgrelid IN('photo.media_retry_jobs'::regclass,'photo.download_intents'::regclass)
  AND t.tgname='historical_item_identity' AND t.tgenabled='O' AND t.tgtype=19 AND NOT t.tgisinternal
  AND t.tgfoid='photo.reject_content_update()'::regprocedure)
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.accept_media_retry_command(uuid,uuid,uuid,uuid,jsonb)') AND p.prosecdef
  AND pg_get_userbyid(p.proowner)='photo_fn_owner' AND has_function_privilege('photo_worker',p.oid,'EXECUTE')
  AND strpos(p.prosrc,'IF FOUND THEN RETURN old.tenant_id=b.tenant_id')>0
  AND strpos(p.prosrc,'IF FOUND THEN RETURN old.tenant_id=b.tenant_id')<strpos(p.prosrc,'PERFORM i.id FROM photo.collection_items i')
  AND strpos(p.prosrc,'IF NOT FOUND THEN RETURN false;END IF;')>0
  AND strpos(p.prosrc,'i.id=p_item AND i.asset_version_id=p_version FOR SHARE OF i')>0
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
 AND EXISTS(SELECT FROM pg_trigger t WHERE t.tgrelid='photo.publications'::regclass AND t.tgname='immutable_content' AND t.tgenabled='O' AND NOT t.tgisinternal
  AND t.tgfoid='photo.reject_content_update()'::regprocedure AND t.tgnargs=4
  AND strpos(pg_get_triggerdef(t.oid),'''tenant_id'', ''id'', ''item_id'', ''asset_version_id''')>0)
 AND EXISTS(SELECT FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid
  WHERE t.tgrelid='photo.publications'::regclass AND t.tgname='current_item_version' AND t.tgenabled='O' AND t.tgtype=7 AND NOT t.tgisinternal
  AND p.oid=to_regprocedure('photo.guard_publication_current_item_version()') AND NOT p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'i.asset_version_id=NEW.asset_version_id FOR SHARE OF i')>0
  AND strpos(p.prosrc,'PUBLICATION_CURRENT_ITEM_VERSION_REQUIRED')>0
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee<>p.proowner AND a.privilege_type='EXECUTE'))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.process_bulk_chunk(uuid,uuid)') AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo'
  AND has_function_privilege('photo_worker',p.oid,'EXECUTE')
  AND strpos(p.prosrc,'ON CONFLICT(tenant_id,item_id,asset_version_id) DO UPDATE SET state=''published''')>0
  AND strpos(p.prosrc,'ON CONFLICT(tenant_id,item_id)')=0
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))`,role).Scan(&ready)
 if err!=nil{return err};if err:=ctx.Err();err!=nil{return err};if !ready{return errors.New("collection publication history schema unavailable")};return nil
}
