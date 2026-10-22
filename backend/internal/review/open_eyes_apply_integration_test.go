package review

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// Requires an isolated migrated database. Authored, not executed during coding.
func TestIntegrationOpenEyesSelectedApplyAuthority(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn=="" { t.Skip("isolated database migrated through 02478 required") }
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 conn,err:=pgx.Connect(ctx,dsn);if err!=nil {t.Fatal(err)};defer conn.Close(context.WithoutCancel(ctx))
 for _,tc:=range []struct{signature string;api bool}{
  {"photo.v28_open_eyes_selected_authority(uuid,uuid,uuid,uuid)",false},
  {"photo.apply_v28_open_eyes_version(uuid,uuid,uuid,uuid,text,text,text)",true},
 } {
  var owner string;var secdef,api,worker,scheduler,portal,gateway,maintenance,public bool
  err=conn.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef,
   has_function_privilege('photo_api',p.oid,'EXECUTE'),has_function_privilege('photo_worker',p.oid,'EXECUTE'),
   has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),has_function_privilege('photo_portal',p.oid,'EXECUTE'),
   has_function_privilege('photo_gateway',p.oid,'EXECUTE'),has_function_privilege('photo_maintenance',p.oid,'EXECUTE'),
   EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
   FROM pg_proc p WHERE p.oid=to_regprocedure($1)`,tc.signature).
   Scan(&owner,&secdef,&api,&worker,&scheduler,&portal,&gateway,&maintenance,&public)
  if err!=nil {t.Fatal(err)}
  if owner!="photo_fn_owner"||!secdef||api!=tc.api||worker||scheduler||portal||gateway||maintenance||public {
   t.Fatal("selected image authority exposed",tc.signature)
  }
 }
 tx,err:=conn.Begin(ctx);if err!=nil {t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil {t.Fatal(err)}
 tid,actor,session,reviewID:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tid.String());err!=nil {t.Fatal(err)}
 for _,otherTenant:=range []uuid.UUID{tid,uuid.New()} {
  var version *uuid.UUID
  if err=tx.QueryRow(ctx,`SELECT photo.apply_v28_open_eyes_version($1,$2,$3,$4,$5,$6,$7)`,
   otherTenant,reviewID,actor,session,strings.Repeat("a",64),`{}`,`{}`).Scan(&version);err!=nil {t.Fatal(err)}
  if version!=nil {t.Fatal("unbound actor/review or cross-tenant request materialized a version")}
 }
 var count int
 if err=tx.QueryRow(ctx,`SELECT count(*) FROM photo.asset_versions WHERE tenant_id=$1 AND id=$2`,tid,
  uuid.NewSHA1(reviewID,[]byte("V28.OpenEyesAppliedVersion"))).Scan(&count);err!=nil {t.Fatal(err)}
 if count!=0 {t.Fatal("rejected request inserted a version")}
}
