package collection

import (
 "testing"
 "time"
 "strings"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/pagination"
)

// Table-lock waiting isolates the interval after collection authorization but
// before the upload SELECT completes; no production helper is replaced.
func TestIntegrationGuestUploadListReauthorizesAfterQueryWait(t *testing.T){
 ctx,admin,service,_,tid,cid,_:=sectionAbandonFixture(t)
 service.Cursors=pagination.Codec{Key:[]byte(strings.Repeat("c",32))}
 var user,project uuid.UUID;var epoch int64
 if e:=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&user,&epoch);e!=nil{t.Fatal(e)}
 if _,e:=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')`,tid,user);e!=nil{t.Fatal(e)}
 session:=uuid.New()
 if _,e:=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',clock_timestamp()+interval '1 hour')`,session,user,auth.HashSecret(uuid.NewString()),uuid.New());e!=nil{t.Fatal(e)}
 if e:=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,cid).Scan(&project);e!=nil{t.Fatal(e)}
 if _,e:=admin.Exec(ctx,`UPDATE photo.projects SET visibility='restricted' WHERE tenant_id=$1 AND id=$2`,tid,project);e!=nil{t.Fatal(e)}
 who:=auth.Identity{UserID:user,SessionID:session,Epoch:epoch}
 if _,e:=service.ListGuestUploads(ctx,who,tid,cid,100,"");e!=nil{t.Fatal("authorized initial observation",e)}
 held,e:=admin.Begin(ctx);if e!=nil{t.Fatal(e)};defer held.Rollback(ctx)
 var blocker int32;if e=held.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);e!=nil{t.Fatal(e)}
 if _,e=held.Exec(ctx,`LOCK TABLE photo.upload_sessions IN ACCESS EXCLUSIVE MODE`);e!=nil{t.Fatal(e)}
 done:=make(chan error,1);go func(){_,err:=service.ListGuestUploads(ctx,who,tid,cid,100,"");done<-err}()
 tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop()
 for {var waiting bool
  if e=admin.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)))`,blocker).Scan(&waiting);e!=nil{t.Fatal(e)}
  if waiting{break};select{case err:=<-done:t.Fatal("upload SELECT did not wait",err);case <-ctx.Done():t.Fatal(ctx.Err());case <-tick.C:}
 }
 if _,e=admin.Exec(ctx,`UPDATE photo.memberships SET role='editor' WHERE tenant_id=$1 AND user_id=$2`,tid,user);e!=nil{t.Fatal(e)}
 if e=held.Commit(ctx);e!=nil{t.Fatal(e)}
 if err:=<-done;err==nil{t.Fatal("list disclosed metadata after restricted project access withdrawal")}
}
