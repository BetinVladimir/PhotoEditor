package project

import (
 "context"
 "bytes"
 "encoding/json"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/domain"
)

// Authored actual two-connection scenario; no execution evidence.
func TestIntegrationProjectUpdateReauthorizesAfterProjectLock(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated current migrated PostgreSQL required")}
 for _,scenario:=range []string{"admin_demoted_without_restricted_acl","stale_cas"}{t.Run(scenario,func(t *testing.T){
  ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
  admin,e:=pgxpool.New(ctx,dsn);if e!=nil{t.Fatal(e)};defer admin.Close()
  runtimeDSN:=os.Getenv("PHOTO_TEST_RUNTIME_DATABASE_URL");if runtimeDSN==""{t.Fatal("isolated nonprivileged API runtime connection required")};config,e:=pgxpool.ParseConfig(runtimeDSN);if e!=nil{t.Fatal(e)};config.ConnConfig.RuntimeParams["role"]="photo_api";database.GuardApplicationRole(config)
  api,e:=pgxpool.NewWithConfig(ctx,config);if e!=nil{t.Fatal(e)};defer api.Close()
  var owner,user,tid,pid uuid.UUID;var epoch int64
  if e=admin.QueryRow(ctx,`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&owner);e!=nil{t.Fatal(e)}
  if e=admin.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,owner).Scan(&tid);e!=nil{t.Fatal(e)}
  if e=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&user,&epoch);e!=nil{t.Fatal(e)}
  if _,e=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')`,tid,user);e!=nil{t.Fatal(e)}
  session:=uuid.New();if _,e=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,user,auth.HashSecret(uuid.NewString()),uuid.New());e!=nil{t.Fatal(e)}
  if e=admin.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Original','restricted') RETURNING id`,tid).Scan(&pid);e!=nil{t.Fatal(e)}
  holder,e:=admin.Begin(ctx);if e!=nil{t.Fatal(e)};defer holder.Rollback(ctx)
  var blocker int;if e=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);e!=nil{t.Fatal(e)}
  var held uuid.UUID;if e=holder.QueryRow(ctx,`SELECT id FROM photo.projects WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,pid).Scan(&held);e!=nil{t.Fatal(e)}
  if scenario=="stale_cas"{if _,e=holder.Exec(ctx,`UPDATE photo.projects SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,pid);e!=nil{t.Fatal(e)}}
  svc:=Service{DB:api};who:=auth.Identity{UserID:user,SessionID:session,Epoch:epoch};name:="Should not commit"
  done:=make(chan error,1);go func(){_,err:=svc.Update(ctx,who,tid,pid,1,Patch{Name:&name});done<-err}()
  deadline:=time.NewTimer(3*time.Second);defer deadline.Stop();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop();observed:=false
  for !observed{select{case err:=<-done:t.Fatal("project lock wait not reached",err);case <-deadline.C:t.Fatal("project lock wait not observed");case <-tick.C:if e=admin.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,blocker).Scan(&observed);e!=nil{t.Fatal(e)}}}
  if scenario=="admin_demoted_without_restricted_acl"{if _,e=admin.Exec(ctx,`UPDATE photo.memberships SET role='editor' WHERE tenant_id=$1 AND user_id=$2`,tid,user);e!=nil{t.Fatal(e)}}
  if e=holder.Commit(ctx);e!=nil{t.Fatal(e)}
  expected:="NOT_FOUND";wantRevision:=int64(1);if scenario=="stale_cas"{expected="REVISION_CONFLICT";wantRevision=2}
  select{case e=<-done:if domain.AsError(e).Code!=expected{t.Fatal("wrong post-wait refusal",expected,e)};case <-ctx.Done():t.Fatal(ctx.Err())}
  var stored string;var revision int64;if e=admin.QueryRow(ctx,`SELECT name,revision FROM photo.projects WHERE tenant_id=$1 AND id=$2`,tid,pid).Scan(&stored,&revision);e!=nil||stored!="Original"||revision!=wantRevision{t.Fatal("refused patch changed project",stored,revision,e)}
 })}
}

func TestIntegrationProjectCreateReplayKeepsReceiptButRequiresCurrentProjectAccess(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated current migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 admin,e:=pgxpool.New(ctx,dsn);if e!=nil{t.Fatal(e)};defer admin.Close()
 runtimeDSN:=os.Getenv("PHOTO_TEST_RUNTIME_DATABASE_URL");if runtimeDSN==""{t.Fatal("isolated nonprivileged API runtime connection required")};config,e:=pgxpool.ParseConfig(runtimeDSN);if e!=nil{t.Fatal(e)};config.ConnConfig.RuntimeParams["role"]="photo_api";database.GuardApplicationRole(config)
 api,e:=pgxpool.NewWithConfig(ctx,config);if e!=nil{t.Fatal(e)};defer api.Close()
 var owner,user,tid uuid.UUID;var epoch int64
 if e=admin.QueryRow(ctx,`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&owner);e!=nil{t.Fatal(e)}
 if e=admin.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,owner).Scan(&tid);e!=nil{t.Fatal(e)}
 if e=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&user,&epoch);e!=nil{t.Fatal(e)}
 if _,e=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'editor','active')`,tid,user);e!=nil{t.Fatal(e)}
 session:=uuid.New();if _,e=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,user,auth.HashSecret(uuid.NewString()),uuid.New());e!=nil{t.Fatal(e)}
 svc:=Service{DB:api};who:=auth.Identity{UserID:user,SessionID:session,Epoch:epoch};key:=uuid.NewString();req:=Create{Name:"Immutable accepted project",Visibility:"restricted",DownloadAllowed:true}
 first,e:=svc.Create(ctx,who,tid,key,req);if e!=nil||first.Status!=201||first.ResourceID==nil{t.Fatal("first project acceptance",first,e)}
 var receipt Project;if e=json.Unmarshal(first.Body,&receipt);e!=nil{t.Fatal(e)}
 replay,e:=svc.Create(ctx,who,tid,key,req);if e!=nil||!replay.Replayed||!bytes.Equal(first.Body,replay.Body){t.Fatal("exact authorized creation replay",replay,e)}
 if _,e=admin.Exec(ctx,`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,receipt.ID,user);e!=nil{t.Fatal(e)}
 denied,e:=svc.Create(ctx,who,tid,key,req);if domain.AsError(e).Code!="NOT_FOUND"||len(denied.Body)!=0{t.Fatal("revoked creation replay disclosed receipt",denied,e)}
 var projects,receipts int
 if e=admin.QueryRow(ctx,`SELECT count(*) FROM photo.projects WHERE tenant_id=$1 AND name=$2`,tid,req.Name).Scan(&projects);e!=nil||projects!=1{t.Fatal("replay re-created project",projects,e)}
 if e=admin.QueryRow(ctx,`SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Project.Create' AND key=$3 AND response_status=201`,tid,user,key).Scan(&receipts);e!=nil||receipts!=1{t.Fatal("refusal lost accepted receipt",receipts,e)}
 if _,e=admin.Exec(ctx,`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'editor')`,tid,receipt.ID,user);e!=nil{t.Fatal(e)}
 replay,e=svc.Create(ctx,who,tid,key,req);if e!=nil||!bytes.Equal(first.Body,replay.Body){t.Fatal("restored authority must recover original receipt",e)}
}
