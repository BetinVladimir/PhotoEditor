package tenant

import (
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// Real PostgreSQL concurrency regression. Requires the normal isolated tenant
// integration fixture; no mocked authorization or SQL execution transcript.
func TestIntegrationUpdateMemberRechecksTargetWaitAuthority(t *testing.T) {
 for _, change := range []string{"revoke", "natural_expiry"} { t.Run(change,func(t *testing.T) {
  s,admin,who,tid,target:=tenantFixture(t)
  ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
  holder,err:=admin.Begin(ctx);if err!=nil {t.Fatal(err)};defer holder.Rollback(context.Background())
  var pid int; if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil {t.Fatal(err)}
  var originalRole,originalStatus string;var originalRevision int64
  if err=holder.QueryRow(ctx,`SELECT role,status,revision FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2 FOR UPDATE`,tid,target).Scan(&originalRole,&originalStatus,&originalRevision);err!=nil {t.Fatal(err)}
  done:=make(chan error,1)
  go func(){_,e:=s.UpdateMember(ctx,who,tid,target,originalRevision,domain.Viewer,"suspended");done<-e}()
  awaitAdminLock(t,ctx,admin,pid,done) // Actual pg_blocking_pids, not a sleep.
  if change=="revoke" {
   _,err=admin.Exec(ctx,`UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`,who.SessionID)
  } else {
   _,err=admin.Exec(ctx,`UPDATE photo.sessions SET expires_at=clock_timestamp()+interval '100 milliseconds' WHERE id=$1`,who.SessionID)
  }
  if err!=nil {t.Fatal(err)}
  if change=="natural_expiry" {
   tick:=time.NewTicker(5*time.Millisecond);defer tick.Stop()
   for {
    var expired bool
    if err=admin.QueryRow(ctx,`SELECT expires_at<=clock_timestamp() FROM photo.sessions WHERE id=$1`,who.SessionID).Scan(&expired);err!=nil {t.Fatal(err)}
    if expired {break}
    select {case <-ctx.Done():t.Fatal(ctx.Err());case <-tick.C:}
   }
   awaitAdminLock(t,ctx,admin,pid,done) // Still actually blocked after expiry.
  }
  if err=holder.Commit(ctx);err!=nil {t.Fatal(err)}
  select {case err=<-done:case <-ctx.Done():t.Fatal(ctx.Err())}
  if domain.AsError(err).Code!="NOT_FOUND" {t.Fatal("withdrawn authority accepted",err)}
  var role,status string;var revision int64
  if err=admin.QueryRow(ctx,`SELECT role,status,revision FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2`,tid,target).Scan(&role,&status,&revision);err!=nil {t.Fatal(err)}
  if role!=originalRole||status!=originalStatus||revision!=originalRevision {t.Fatal("refused update changed target",role,status,revision)}
 })}
}

func TestIntegrationUpdateMemberSelfDemotionAndSuspension(t *testing.T) {
 for _,status:=range []string{"active","suspended"} {t.Run(status,func(t *testing.T){
  s,admin,_,tid,target:=tenantFixture(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
  if _,err:=admin.Exec(ctx,`UPDATE photo.memberships SET role='admin' WHERE tenant_id=$1 AND user_id=$2`,tid,target);err!=nil {t.Fatal(err)}
  sid:=uuid.New()
  if _,err:=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',clock_timestamp()+interval '1 hour')`,sid,target,auth.HashSecret(uuid.NewString()),uuid.New());err!=nil {t.Fatal(err)}
  var revision int64;var email string
  if err:=admin.QueryRow(ctx,`SELECT m.revision,u.email_normalized FROM photo.memberships m JOIN photo.users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.user_id=$2`,tid,target).Scan(&revision,&email);err!=nil {t.Fatal(err)}
  got,err:=s.UpdateMember(ctx,auth.Identity{UserID:target,SessionID:sid},tid,target,revision,domain.Viewer,status)
  if err!=nil {t.Fatal(err)}
  if got.Email!=email||got.Role!=domain.Viewer||got.Status!=status {t.Fatal("self authority change/email lost",got)}
  var actualRole,actualStatus string;var actualRevision int64
  if err=admin.QueryRow(ctx,`SELECT role,status,revision FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2`,tid,target).Scan(&actualRole,&actualStatus,&actualRevision);err!=nil {t.Fatal(err)}
  if actualRole!="viewer"||actualStatus!=status||actualRevision!=revision+1 {t.Fatal(actualRole,actualStatus,actualRevision)}
 })}
}
