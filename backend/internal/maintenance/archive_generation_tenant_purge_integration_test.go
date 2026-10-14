package maintenance

import (
 "context"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationArchiveGenerationTenantPurgeSettlementAuthority(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn=="" { t.Skip("isolated DB migrations through 01810 required") }
 ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil { t.Fatal(err) };defer db.Close()
 const signature="photo.settle_v30_archive_generations_after_tenant_purge(uuid,uuid,uuid)"
 var owner,definition string
 var secdef bool
 if err:=db.QueryRow(ctx,`SELECT pg_get_userbyid(proowner),prosecdef,pg_get_functiondef(oid) FROM pg_proc WHERE oid=$1::regprocedure`,signature).Scan(&owner,&secdef,&definition);err!=nil { t.Fatal(err) }
 if owner!="photo_fn_owner"||!secdef { t.Fatal("settlement authority") }
 for _,required:=range []string{"cleanup_lease_until>clock_timestamp()","status='deleting'","state='open'","state IN('creating','create_failed')","terminal_evidence<>'s3-create-receipt'","purged_at=clock_timestamp()","LIMIT 256 FOR UPDATE","RETURN 'more'"} {
  if !strings.Contains(definition,required) { t.Fatal("settlement fence absent",required) }
 }
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal","photo_maintenance"} {
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil { t.Fatal(err) }
  if allowed!=(role=="photo_maintenance") { t.Fatal("settlement role",role,allowed) }
 }
 var state string
 if err:=db.QueryRow(ctx,`SELECT photo.settle_v30_archive_generations_after_tenant_purge($1,$2,$3)`,uuid.New(),uuid.New(),uuid.New()).Scan(&state);err!=nil||state!="blocked" { t.Fatal("unleased settlement accepted",state,err) }
}
