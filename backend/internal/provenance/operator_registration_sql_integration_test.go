package provenance

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationAPISignerRegistrationScopePinsAndLifecycle(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated DB required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 exec:=func(query string,args ...any){t.Helper();if _,err:=tx.Exec(ctx,query,args...);err!=nil{t.Fatal(err)}}
 var user,tenant uuid.UUID;var epoch int64
 if err=tx.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@api-signer.test").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,user).Scan(&tenant);err!=nil{t.Fatal(err)}
 worker:=uuid.New();a,b,c,d:=strings.Repeat("a",64),strings.Repeat("b",64),strings.Repeat("c",64),strings.Repeat("d",64)
 exec(`INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,enabled,db_principal,qualified_operations,execution_pool,signer_sdk_lock_sha,signer_certificate_chain_sha,signer_trust_bundle_sha,signer_qualification_sha,signer_qualified_until)
 VALUES($1,$2,'["cpu"]','["cpu"]','',clock_timestamp(),'{}',true,session_user,'{"provenance.sign":["cpu"]}','signer',$3,$4,$5,$6,clock_timestamp()+interval '1 hour')`,tenant,worker,a,b,c,d)
 check:=func(scope string,tid,wid uuid.UUID,sdk,cert,trust,qualification string,want bool){
  t.Helper()
  exec(`SELECT set_config('app.tenant_id',$1,true)`,scope)
  exec(`SET LOCAL ROLE photo_api`)
  var ready bool
  err:=tx.QueryRow(ctx,`SELECT photo.v31_api_signer_registration_ready($1,$2,$3,$4,$5,$6)`,tid,wid,sdk,cert,trust,qualification).Scan(&ready)
  if err!=nil{t.Fatal(err)}
  exec(`RESET ROLE`)
  if ready!=want{t.Fatalf("registration ready=%v want=%v",ready,want)}
 }
 check(tenant.String(),tenant,worker,a,b,c,d,true)
 check("",tenant,worker,a,b,c,d,false)
 check(uuid.NewString(),tenant,worker,a,b,c,d,false)
 check(tenant.String(),uuid.New(),worker,a,b,c,d,false)
 check(tenant.String(),tenant,uuid.New(),a,b,c,d,false)
 check(tenant.String(),tenant,worker,b,b,c,d,false)
 check(tenant.String(),tenant,worker,a,a,c,d,false)
 check(tenant.String(),tenant,worker,a,b,a,d,false)
 check(tenant.String(),tenant,worker,a,b,c,a,false)
 for _,mutation:=range []string{
  `enabled=false`,
  `signer_qualified_until=clock_timestamp()-interval '1 second'`,
  `last_seen=clock_timestamp()-interval '31 seconds'`,
 }{
  exec(`UPDATE photo.worker_registrations SET enabled=true,signer_qualified_until=clock_timestamp()+interval '1 hour',last_seen=clock_timestamp() WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
  exec(`UPDATE photo.worker_registrations SET `+mutation+` WHERE tenant_id=$1 AND worker_id=$2`,tenant,worker)
  check(tenant.String(),tenant,worker,a,b,c,d,false)
 }
 var public,workerAccess,schedulerAccess,api bool
 if err=tx.QueryRow(ctx,`SELECT has_function_privilege('photo_api',p.oid,'EXECUTE'),has_function_privilege('photo_worker',p.oid,'EXECUTE'),has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') FROM pg_proc p WHERE p.oid=to_regprocedure('photo.v31_api_signer_registration_ready(uuid,uuid,text,text,text,text)')`).Scan(&api,&workerAccess,&schedulerAccess,&public);err!=nil||!api||workerAccess||schedulerAccess||public{t.Fatal("registration probe ACL",api,workerAccess,schedulerAccess,public,err)}
}
