package tenant

import (
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
)

func awaitAdminLock(t *testing.T,ctx context.Context,db *pgxpool.Pool,pid int,done <-chan error) {
 t.Helper();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop()
 for {select {case err:=<-done:t.Fatal("operation completed before authority wait",err);case <-ctx.Done():t.Fatal(ctx.Err());case <-tick.C:
 var waiting bool;if err:=db.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,pid).Scan(&waiting);err!=nil{t.Fatal(err)};if waiting{return}
 }}
}

func TestIntegrationInvitationReceiptSurvivesRetiredMailKeys(t *testing.T) {
 s,admin,who,tid,_:=tenantFixture(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 s.MailKeys=auth.KeyRing{Active:"test",Keys:map[string][]byte{"test":make([]byte,32)}}
 request:=InviteRequest{Email:uuid.NewString()+"@invite.test",Role:domain.Editor,Locale:"en"};key:=uuid.NewString()
 first,err:=s.Invite(ctx,who,tid,key,request);if err!=nil||first.ID==uuid.Nil{t.Fatal(first,err)}
 s.MailKeys=auth.KeyRing{}
 replay,err:=s.Invite(ctx,who,tid,key,request);if err!=nil||replay!=first{t.Fatal("accepted receipt depended on mail configuration",replay,err)}
 changed:=request;changed.Role=domain.Viewer
 if _,err=s.Invite(ctx,who,tid,key,changed);domain.AsError(err).Code!="IDEMPOTENCY_CONFLICT"{t.Fatal(err)}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.invitations WHERE tenant_id=$1`,tid).Scan(&count);err!=nil||count!=1{t.Fatal(count,err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`,who.SessionID);err!=nil{t.Fatal(err)}
 if _,err=s.Invite(ctx,who,tid,key,request);domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("receipt bypassed live authority",err)}
}

func TestIntegrationInvitationWaitRechecksSession(t *testing.T) {
 s,admin,who,tid,_:=tenantFixture(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 s.MailKeys=auth.KeyRing{Active:"test",Keys:map[string][]byte{"test":make([]byte,32)}}
 request:=InviteRequest{Email:uuid.NewString()+"@invite.test",Role:domain.Editor,Locale:"en"};key:=uuid.NewString();hash,err:=canonical.Hash(request);if err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,'tenant.invite',$3,$4,now()+interval '1 day')`,tid,who.UserID,key,hash);err!=nil{t.Fatal(err)}
 holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(context.Background())
 var pid int;if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil{t.Fatal(err)}
 var id uuid.UUID;if err=holder.QueryRow(ctx,`SELECT id FROM photo.idempotency_records WHERE tenant_id=$1 AND key=$2 FOR UPDATE`,tid,key).Scan(&id);err!=nil{t.Fatal(err)}
 done:=make(chan error,1);go func(){_,e:=s.Invite(ctx,who,tid,key,request);done<-e}();awaitAdminLock(t,ctx,admin,pid,done)
 if _,err=admin.Exec(ctx,`UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`,who.SessionID);err!=nil{t.Fatal(err)}
 if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
 if err=<-done;domain.AsError(err).Code!="NOT_FOUND"{t.Fatal(err)}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.invitations WHERE tenant_id=$1`,tid).Scan(&count);err!=nil||count!=0{t.Fatal(count,err)}
}

func TestIntegrationTenantDeleteAndMemberRemovalRecheckWaitedAuthority(t *testing.T) {
 for _,action:=range []string{"delete","remove"}{t.Run(action,func(t *testing.T){
 s,admin,who,tid,target:=tenantFixture(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(context.Background())
 var pid int;if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil{t.Fatal(err)}
 var id uuid.UUID
 if action=="delete"{err=holder.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE id=$1 FOR UPDATE`,tid).Scan(&id)}else{err=holder.QueryRow(ctx,`SELECT user_id FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2 FOR UPDATE`,tid,target).Scan(&id)};if err!=nil{t.Fatal(err)}
 done:=make(chan error,1);go func(){if action=="delete"{_,e:=s.Delete(ctx,who,tid,1);done<-e}else{done<-s.RemoveMember(ctx,who,tid,target,1)}}();awaitAdminLock(t,ctx,admin,pid,done)
 if _,err=admin.Exec(ctx,`UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`,who.SessionID);err!=nil{t.Fatal(err)}
 if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
 if err=<-done;err==nil{t.Fatal("authority withdrawn during wait was accepted")}
 var active bool;if err=admin.QueryRow(ctx,`SELECT status='active' FROM photo.tenants WHERE id=$1`,tid).Scan(&active);err!=nil||!active{t.Fatal(active,err)}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2`,tid,target).Scan(&count);err!=nil||count!=1{t.Fatal(count,err)}
 if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.deletion_jobs WHERE tenant_id=$1`,tid).Scan(&count);err!=nil||count!=0{t.Fatal(count,err)}
 })}
}

func TestIntegrationTenantDeletionRepeatedReceiptAndNullableRefusal(t *testing.T) {
 s,admin,who,tid,_:=tenantFixture(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 first,err:=s.Delete(ctx,who,tid,1);if err!=nil||first.JobID==uuid.Nil||first.Status!="queued"{t.Fatal(first,err)}
 repeated,err:=s.Delete(ctx,who,tid,1);if err!=nil||repeated!=first{t.Fatal("deleting receipt changed",repeated,err)}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.deletion_jobs WHERE tenant_id=$1 AND resource_type='tenant'`,tid).Scan(&count);err!=nil||count!=1{t.Fatal(count,err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`,who.SessionID);err!=nil{t.Fatal(err)}
 if _,err=s.Delete(ctx,who,tid,1);domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("nullable refusal lost domain classification",err)}
}

func TestIntegrationAdminInstallationRejectsACLAndSecurityDrift(t *testing.T) {
 s,admin,_,_,_:=tenantFixture(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 if err:=RequireAdminInstallation(ctx,s.DB);err!=nil{t.Fatal(err)}
 for _,drift:=range []string{
  `GRANT EXECUTE ON FUNCTION photo.read_invitation_receipt(uuid,uuid,uuid,text,text) TO photo_worker`,
  `ALTER FUNCTION photo.read_invitation_receipt(uuid,uuid,uuid,text,text) SECURITY INVOKER`,
 }{func(){
 tx,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,drift);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{t.Fatal(err)}
 if err=RequireAdminInstallation(ctx,tx);err==nil{t.Fatal("drift accepted",drift)}
 }()}
}

// Exercise the actual public SQL writer, not only the receipt-first reader.
func TestIntegrationInvitationWriterRechecksAuthorityAfterIdempotencyWait(t *testing.T) {
 s,admin,who,tid,_:=tenantFixture(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 request:=InviteRequest{Email:uuid.NewString()+"@invite.test",Role:domain.Editor,Locale:"en"};key:=uuid.NewString();hash,err:=canonical.Hash(request);if err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,'tenant.invite',$3,$4,now()+interval '1 day')`,tid,who.UserID,key,hash);err!=nil{t.Fatal(err)}
 holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(context.Background())
 var pid int;if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil{t.Fatal(err)}
 var id uuid.UUID;if err=holder.QueryRow(ctx,`SELECT id FROM photo.idempotency_records WHERE tenant_id=$1 AND key=$2 FOR UPDATE`,tid,key).Scan(&id);err!=nil{t.Fatal(err)}
 var outcome string;done:=make(chan error,1)
 go func(){done<-s.DB.QueryRow(ctx,`SELECT outcome FROM photo.create_invitation($1,$2,$3,$4,$5,'editor',$6,clock_timestamp()+interval '7 days',$7,$8,$8,'test',$9,$10,$11,$12)`,who.UserID,who.SessionID,tid,uuid.New(),request.Email,auth.HashSecret(uuid.NewString()),uuid.New(),[]byte("unused ciphertext"),"<test@photoeditor.local>",uuid.New(),key,hash).Scan(&outcome)}()
 awaitAdminLock(t,ctx,admin,pid,done)
 if _,err=admin.Exec(ctx,`UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`,who.SessionID);err!=nil{t.Fatal(err)}
 if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
 if err=<-done;err!=nil||outcome!="not_found"{t.Fatal(outcome,err)}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.invitations WHERE tenant_id=$1`,tid).Scan(&count);err!=nil||count!=0{t.Fatal(count,err)}
}

func TestIntegrationFreshInvitationRollsBackWhenSessionWithdrawnDuringMailWrite(t *testing.T) {
 s,admin,who,tid,_:=tenantFixture(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 s.MailKeys=auth.KeyRing{Active:"test",Keys:map[string][]byte{"test":make([]byte,32)}}
 holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(context.Background())
 var pid int;if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil{t.Fatal(err)}
 if _,err=holder.Exec(ctx,`LOCK TABLE photo.mail_payloads IN ACCESS EXCLUSIVE MODE`);err!=nil{t.Fatal(err)}
 key:=uuid.NewString();request:=InviteRequest{Email:uuid.NewString()+"@invite.test",Role:domain.Editor,Locale:"en"}
 done:=make(chan error,1);go func(){_,e:=s.Invite(ctx,who,tid,key,request);done<-e}();awaitAdminLock(t,ctx,admin,pid,done)
 if _,err=admin.Exec(ctx,`UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`,who.SessionID);err!=nil{t.Fatal(err)}
 if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
 if err=<-done;domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("post-write authority loss was not classified",err)}
 var count int
 if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.invitations WHERE tenant_id=$1`,tid).Scan(&count);err!=nil||count!=0{t.Fatal("invitation survived refused statement",count,err)}
 if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='tenant.invite' AND key=$3`,tid,who.UserID,key).Scan(&count);err!=nil||count!=0{t.Fatal("receipt survived refused statement",count,err)}
 if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1 AND action='invitation.create'`,tid).Scan(&count);err!=nil||count!=0{t.Fatal("audit survived refused statement",count,err)}
}
