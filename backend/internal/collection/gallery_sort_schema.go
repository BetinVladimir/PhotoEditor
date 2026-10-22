package collection

import (
 "context"
 "errors"
 "time"

 "photoeditor/backend/internal/artifactjson"
)

// RequireGallerySort checks the installed functions, not migration names. The
// query runs once at startup; gallery requests never load image/GPU resources.
func RequireGallerySort(ctx context.Context, db artifactjson.RowQuerier) error {
 if err:=ctx.Err();err!=nil{return err}
 if db==nil{return errors.New("collection gallery database required")}
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 var ready bool
 err:=db.QueryRow(ctx,`SELECT current_user='photo_api'
 AND (SELECT count(*)=2 FROM pg_proc p WHERE p.oid IN(
   to_regprocedure('photo.collection_capture_instant(text)'),
   to_regprocedure('photo.collection_item_order(uuid,uuid)'))
  AND NOT p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND has_function_privilege(current_user,p.oid,'EXECUTE')
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
   WHERE a.privilege_type='EXECUTE' AND a.grantee NOT IN(p.proowner,(SELECT oid FROM pg_roles WHERE rolname='photo_api'))))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.collection_item_order(uuid,uuid)')
  AND p.prorettype='record'::regtype AND p.proretset
  AND strpos(p.prosrc,'row_number() OVER(ORDER BY')>0
  AND strpos(p.prosrc,'c.sort_mode=''captured_at''')>0
  AND strpos(p.prosrc,'c.sort_mode=''filename''')>0
  AND strpos(p.prosrc,'b.source_sha=original.sha256')>0
  AND strpos(p.prosrc,'u.ingest_run_id=b.run_id')>0
  AND strpos(p.prosrc,'r.content_sha=b.source_sha')>0
  AND strpos(p.prosrc,'current_setting(''app.tenant_id'',true)')>0
  AND strpos(p.prosrc,'i.position,i.id')>0)
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.list_portal_items(bytea,uuid,bigint,uuid,integer)')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'photo.collection_item_order(c.tenant_id,c.id)')>0
  AND strpos(p.prosrc,'ordering.sort_ordinal')>0
  AND strpos(p.prosrc,'i.position')=0
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.adopt_ingest_image(uuid,uuid,bigint,uuid,text,jsonb,jsonb)')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'-''captured_at''-''captured_local''')>0
  AND strpos(p.prosrc,'p_metadata ? ''captured_local''')>0
  AND strpos(p.prosrc,'photo.collection_capture_instant(p_metadata->>''captured_at'')')>0)`).Scan(&ready)
 if err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 if !ready{return errors.New("collection gallery sort schema unavailable")}
 return nil
}
