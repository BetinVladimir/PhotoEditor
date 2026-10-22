package queue

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// RequireOutboxMarkFence prevents dispatch against the pre-lock-clock mark implementation.
func RequireOutboxMarkFence(ctx context.Context,db artifactjson.RowQuerier) error {
 if db==nil{return errors.New("outbox database required")}
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 if err:=ctx.Err();err!=nil{return err}
 var ready bool
 err:=db.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_proc p
 WHERE p.oid=to_regprocedure('photo.mark_outbox_sent(text,uuid,timestamp with time zone)')
 AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND strpos(p.prosrc,'-- 02542:')>0
 AND strpos(p.prosrc,'FROM photo.outbox WHERE id=p_id FOR UPDATE;')>0
 AND strpos(p.prosrc,'FROM photo.auth_mail_outbox WHERE id=p_id FOR UPDATE;')>0
 AND strpos(p.prosrc,'held_until<=clock_timestamp()')>strpos(p.prosrc,'FROM photo.auth_mail_outbox WHERE id=p_id FOR UPDATE;')
 AND strpos(p.prosrc,'UPDATE photo.outbox SET')>strpos(p.prosrc,'held_until<=clock_timestamp()')
 AND strpos(p.prosrc,'UPDATE photo.auth_mail_outbox SET')>strpos(p.prosrc,'held_until<=clock_timestamp()')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
  WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'))`).Scan(&ready)
 if err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 if !ready{return errors.New("outbox mark lease fence schema unavailable")}
 return nil
}
