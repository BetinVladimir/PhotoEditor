package maintenance

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// RequireArtifactGCAccounting refuses to run the cleaner on a pre-settlement schema.
func RequireArtifactGCAccounting(ctx context.Context, db artifactjson.RowQuerier) error {
 if db == nil { return errors.New("artifact GC accounting database required") }
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 if err:=ctx.Err();err!=nil{return err}
 var ready bool
 err:=db.QueryRow(ctx,`SELECT
 EXISTS(SELECT FROM pg_attribute a WHERE a.attrelid=to_regclass('photo.artifacts')
  AND a.attname='gc_storage_settled' AND a.atttypid='boolean'::regtype AND a.attnotnull AND NOT a.attisdropped)
 AND EXISTS(SELECT FROM pg_trigger t JOIN pg_attribute a ON a.attrelid=t.tgrelid AND a.attname='gc_storage_settled'
  JOIN pg_proc p ON p.oid=t.tgfoid
  WHERE t.tgrelid=to_regclass('photo.artifacts') AND t.tgname='artifact_gc_settlement_private'
   AND NOT t.tgisinternal AND t.tgenabled IN('O','A') AND t.tgtype=23 AND t.tgnargs=0
   AND t.tgattr::text=a.attnum::text
   AND p.oid=to_regprocedure('photo.guard_artifact_gc_settlement()')
   AND NOT p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
   AND strpos(p.prosrc,'ARTIFACT_GC_SETTLEMENT_PRIVATE')>0 AND strpos(p.prosrc,'current_user')>0)
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.finish_artifact_gc(uuid,uuid)')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND has_function_privilege(current_user,p.oid,'EXECUTE')
  AND strpos(p.prosrc,'-- 02539:')>0
  AND strpos(p.prosrc,'storage_bytes=storage_bytes-charged')>0
  AND strpos(p.prosrc,'gc_storage_settled=true')>strpos(p.prosrc,'storage_bytes=storage_bytes-charged')
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
    WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'))
 AND (SELECT count(*)=2 FROM pg_proc p WHERE p.oid IN(
   to_regprocedure('photo.finish_original_cleanup(uuid,uuid)'),to_regprocedure('photo.finish_staging_cleanup(uuid,uuid)'))
   AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
   AND strpos(p.prosrc,'storage_bytes=storage_bytes-')>0 AND strpos(p.prosrc,'gc_storage_settled=true')>0
   AND strpos(p.prosrc,'-- 02539: repeat cleanup clock after artifact row-lock wait.')>0
   AND strpos(p.prosrc,'-- 02539: repeat cleanup clock after artifact row-lock wait.')<strpos(p.prosrc,'storage_bytes=storage_bytes-'))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.claim_artifact_gc()')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'-- 02539: generic GC must leave dedicated ingest storage')>0
  AND strpos(p.prosrc,'photo.ingest_originals')>0 AND strpos(p.prosrc,'photo.staging_cleanup')>0
  AND strpos(p.prosrc,'photo.ingest_upload_bindings')>0
  AND strpos(p.prosrc,'PERFORM photo.artifact_gc_charged_bytes(a.tenant_id,a.id);')>0)
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.artifact_gc_charged_bytes(uuid,uuid)')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'ARTIFACT_ACCOUNTING_DEDICATED_INGEST')>0
  AND strpos(p.prosrc,'photo.v30_export_snapshot_documents')>0
  AND strpos(p.prosrc,'ARTIFACT_ACCOUNTING_PROVENANCE_UNKNOWN')>0
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
    WHERE acl.grantee<>p.proowner AND acl.privilege_type='EXECUTE'))`).Scan(&ready)
 if err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 if !ready{return errors.New("artifact GC storage settlement schema unavailable")}
 return nil
}
