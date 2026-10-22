package project

import (
 "bytes"
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
)

func TestIntegrationExpiredCompletedReceiptRetainsIdentityAndCurrentProjectACL(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
 runtimeDSN:=os.Getenv("PHOTO_TEST_RUNTIME_DATABASE_URL");if runtimeDSN==""{t.Fatal("isolated nonprivileged API runtime connection required")};config,err:=pgxpool.ParseConfig(runtimeDSN);if err!=nil{t.Fatal(err)};config.ConnConfig.RuntimeParams["role"]="photo_api";database.GuardApplicationRole(config)
 api,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil{t.Fatal(err)};defer api.Close()
 var owner,tid,user uuid.UUID
 if err=admin.QueryRow(ctx,"SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)",uuid.NewString()+"@test.example").Scan(&owner);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,"SELECT id FROM photo.tenants WHERE owner_user_id=$1",owner).Scan(&tid);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,"INSERT INTO photo.users(email_normalized) VALUES($1) RETURNING id",uuid.NewString()+"@test.example").Scan(&user);err!=nil{t.Fatal(err)}
 session,pid:=uuid.New(),uuid.New()
 if _,err=admin.Exec(ctx,"INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'editor')",tid,user);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,"INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',$5)",session,user,auth.HashSecret(uuid.NewString()),uuid.New(),time.Now().Add(time.Hour));err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,"INSERT INTO photo.projects(tenant_id,id,name,visibility) VALUES($1,$2,'retention','restricted')",tid,pid);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,"INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'editor')",tid,pid,user);err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:user,SessionID:session}
 scope:=idempotency.Scope{Tenant:tid,PrincipalType:"user",Principal:user,Route:"Retention.Test",Key:uuid.NewString()}
 invocations:=0
 call:=func(revision int)(idempotency.Response,error){var out idempotency.Response
  e:=tenant.WithTenant(ctx,api,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
   if _,e:=Authorize(ctx,tx,tid,pid,user,role,domain.Editor,false);e!=nil{return e}
   var e error;out,e=idempotency.Use(ctx,tx,scope,map[string]int{"revision":revision},time.Now().Add(time.Hour),func()(idempotency.Response,error){invocations++;return idempotency.Response{Status:202,Body:json.RawMessage(`{"accepted":true}`),ResourceID:&pid},nil});return e
  });return out,e}
 first,err:=call(1);if err!=nil||first.Replayed{t.Fatal("first acceptance",err)}
 if _,err=admin.Exec(ctx,"UPDATE photo.idempotency_records SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND principal_id=$2 AND route=$3 AND key=$4",tid,user,scope.Route,scope.Key);err!=nil{t.Fatal(err)}
 replay,err:=call(1);if err!=nil||!replay.Replayed||replay.Status!=first.Status||!bytes.Equal(replay.Body,first.Body)||invocations!=1{t.Fatal("expired completed receipt did not exact replay",err,invocations)}
 if _,err=call(2);err==nil||domain.AsError(err).Code!="IDEMPOTENCY_CONFLICT"||invocations!=1{t.Fatal("expired fingerprint was reused",err)}
 if _,err=admin.Exec(ctx,"DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3",tid,pid,user);err!=nil{t.Fatal(err)}
 denied,err:=call(1);if err==nil||len(denied.Body)!=0||invocations!=1{t.Fatal("revoked project replay exposed response",err)}
 var count int
 if err=admin.QueryRow(ctx,"SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route=$3 AND key=$4 AND response_status=202",tid,user,scope.Route,scope.Key).Scan(&count);err!=nil||count!=1{t.Fatal("refusal deleted durable acceptance",err,count)}
}
