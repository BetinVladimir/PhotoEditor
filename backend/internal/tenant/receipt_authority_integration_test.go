package tenant

import (
 "context"
 "encoding/json"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

// This models authority disappearing while an admission/replay callback is
// waiting. The successful receipt must roll back with the caller transaction.
func TestIntegrationReceiptAuthorityRecheckedBeforeCommit(t *testing.T) {
 for _, change := range []string{"revoke_session", "expire_session", "suspend_member"} {
  t.Run(change, func(t *testing.T) {
   s, admin, who, tid, target := tenantFixture(t)
   ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
   defer cancel()
   // The tenant owner cannot be suspended by the database invariant. Exercise
   // withdrawal of a real non-owner Editor instead of violating that invariant.
   who.UserID, who.SessionID = target, uuid.New()
   if _, err := admin.Exec(ctx,"INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',$5)",who.SessionID,who.UserID,auth.HashSecret(uuid.NewString()),uuid.New(),time.Now().Add(time.Hour)); err != nil {t.Fatal(err)}
   key := uuid.NewString()
   entered, release := make(chan struct{}), make(chan struct{})
   done := make(chan error, 1)
   go func() {
    done <- WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
     _, err := idempotency.Use(ctx, tx, idempotency.Scope{Tenant:tid, Principal:who.UserID, PrincipalType:"user", Route:"Authority.Test", Key:key}, map[string]string{"intent":"once"}, time.Now().Add(time.Hour), func() (idempotency.Response,error) {
      return idempotency.Response{Status:200, Body:json.RawMessage(`{"accepted":true}`)}, nil
     })
     close(entered)
     select { case <-release: return err; case <-ctx.Done(): return ctx.Err() }
    })
   }()
   select { case <-entered: case <-ctx.Done(): t.Fatal(ctx.Err()) }
   var err error
   switch change {
   case "revoke_session": _, err = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID)
   case "expire_session": _, err = admin.Exec(ctx, "UPDATE photo.sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", who.SessionID)
   case "suspend_member": _, err = admin.Exec(ctx, "UPDATE photo.memberships SET status='suspended' WHERE tenant_id=$1 AND user_id=$2", tid, who.UserID)
   }
   close(release)
   if err != nil { t.Fatal(err) }
   select { case err = <-done: case <-ctx.Done(): t.Fatal(ctx.Err()) }
   if err == nil { t.Fatal("receipt committed using revoked authority") }
   var count int
   if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND key=$2", tid,key).Scan(&count); err != nil { t.Fatal(err) }
   if count != 0 { t.Fatal("refused transaction retained receipt") }
  })
 }
}

func TestIntegrationReceiptParentBindingAndLegacyProof(t *testing.T) {
 s, admin, who, tid, _ := tenantFixture(t)
 ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
 defer cancel()
 key:=uuid.NewString()
 scope:=idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Parent.Test",Key:key}
 call:=func(parent string,legacy func()error)error{
  return WithTenant(ctx,s.DB,who,tid,domain.Viewer,func(tx pgx.Tx,_ domain.Role)error{
   out,err:=idempotency.Use(ctx,tx,scope,map[string]int{"revision":1},time.Now().Add(time.Hour),func()(idempotency.Response,error){return idempotency.Response{Status:201,Body:json.RawMessage(`{"accepted":true}`)},nil})
   if err!=nil{return err}
   return idempotency.BindParent(ctx,tx,scope,out,parent,legacy)
  })
 }
 if err:=call("parent:first",nil);err!=nil{t.Fatal(err)}
 if err:=call("parent:first",nil);err!=nil{t.Fatal(err)}
 if err:=call("parent:other",nil);err==nil{t.Fatal("cross-parent replay accepted")}
 // Simulate a historical receipt without rewriting its fingerprint or key.
 if _,err:=admin.Exec(ctx,"UPDATE photo.idempotency_records SET parent_binding=NULL WHERE tenant_id=$1 AND key=$2",tid,key);err!=nil{t.Fatal(err)}
 if err:=call("parent:first",nil);err==nil{t.Fatal("unproved legacy receipt accepted")}
 proofs:=0
 if err:=call("parent:first",func()error{proofs++;return nil});err!=nil{t.Fatal(err)}
 if proofs!=1{t.Fatal("legacy proof was not required")}
 var stored string
 if err:=admin.QueryRow(ctx,"SELECT parent_binding FROM photo.idempotency_records WHERE tenant_id=$1 AND key=$2",tid,key).Scan(&stored);err!=nil||stored!="parent:first"{t.Fatalf("stored=%q err=%v",stored,err)}
}
