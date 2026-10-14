package interchange
import("context";"os";"testing";"github.com/jackc/pgx/v5/pgxpool")
// ACL coverage only; populated concurrency/expiry scenarios remain deferred.
func TestIntegrationImportRetryClaimPrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL migration01581 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.v30_claim_next_interchange_import()"
 for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_portal"}{
  var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler"){t.Fatal("unexpected retry claim privilege",role,allowed)}
 }
 var secure bool
 if err:=db.QueryRow(ctx,`SELECT p.prosecdef AND owner.rolname='photo_fn_owner' AND 'search_path=pg_catalog, photo'=ANY(p.proconfig)
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
 FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,signature).Scan(&secure);err!=nil{t.Fatal(err)}
 if !secure{t.Fatal("unsafe retry claim SQL installation")}
}

func TestIntegrationImportRetryClaimRejectsDiscoveryBypass(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL migration01581 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,signature:=range []string{"photo.v30_next_interchange_import()","photo.v30_import_discovery_uuid(text)"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege('photo_scheduler',$1,'EXECUTE')`,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed{t.Fatal("scheduler bypass exposed",signature)}
 }
 var direct bool
 if err:=db.QueryRow(ctx,`SELECT has_column_privilege('photo_scheduler','photo.v2_workflows','v30_import_retry_after','UPDATE')`).Scan(&direct);err!=nil{t.Fatal(err)}
 if direct{t.Fatal("scheduler can change retry deadline directly")}
}
func TestIntegrationImportDiscoveryUUIDMalformedMetadata(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL migration01581 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 // Isolated migration administrator can invoke the private helper. Production
 // scheduler cannot, as asserted separately above.
 valid:="73aa09bc-d501-47d9-a88c-44bfb2222a33"
 for _,value:=range []string{"","not-a-uuid",`{"artifact":true}`,"73aa09bc-d501-47d9-a88c-44bfb2222a3z",valid}{
  var accepted bool
  if err:=db.QueryRow(ctx,`SELECT photo.v30_import_discovery_uuid($1) IS NOT NULL`,value).Scan(&accepted);err!=nil{t.Fatal(err)}
  if accepted!=(value==valid){t.Fatal("unexpected discovery UUID",value,accepted)}
 }
 var isNull bool
 if err:=db.QueryRow(ctx,`SELECT photo.v30_import_discovery_uuid(NULL) IS NULL`).Scan(&isNull);err!=nil||!isNull{t.Fatal("missing metadata not refused",err)}
}
