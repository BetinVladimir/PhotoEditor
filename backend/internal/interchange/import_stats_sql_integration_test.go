package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Deferred until migrations are applied to an isolated database. These gates
// do not prove populated import/qualification admission or revocation replay.
func TestIntegrationImportStatsQualificationPrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL migration01582 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.v30_import_stats_qualification_object(uuid,uuid,text,jsonb,uuid,text)"
 for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_portal"}{
  var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler"){t.Fatal("unexpected qualification privilege",role,allowed)}
 }
 var secure bool
 if err:=db.QueryRow(ctx,`SELECT p.prosecdef AND owner.rolname='photo_fn_owner' AND 'search_path=pg_catalog, photo'=ANY(p.proconfig)
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
 FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,signature).Scan(&secure);err!=nil{t.Fatal(err)}
 if !secure{t.Fatal("unsafe qualification SQL installation")}
}
func TestIntegrationImportStatsQualificationRejectsAbsentAndForeignScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL migration01582 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tenant,workflow,report:=uuid.New(),uuid.New(),uuid.New()
 if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{t.Fatal(err)}
 if _,err:=tx.Exec(ctx,`SET LOCAL ROLE photo_scheduler`);err!=nil{t.Fatal(err)}
 for _,scope:=range []uuid.UUID{tenant,uuid.New()}{
  for _,policy:=range []string{`{}`,`{"Operation":"color.stats","Version":"1","LicenseCleared":true}`,`{"Operation":"image.upscale","Version":"1","LicenseCleared":true}`} {
   var count int
   if err:=tx.QueryRow(ctx,`SELECT count(*) FROM photo.v30_import_stats_qualification_object($1,$2,'{}',$3::jsonb,$4,$5)`,scope,workflow,policy,report,rawSHA([]byte("report"))).Scan(&count);err!=nil{t.Fatal(err)}
   if count!=0{t.Fatal("absent/foreign workflow supplied report")}
  }
 }
}
