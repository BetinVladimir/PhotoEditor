package delivery

import (
 "context"
 "os"
 "strings"
 "testing"

 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationDeliverySignedReceiptAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migrations through 01812 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.v30_delivery_signed_receipt(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text)"
 var definition,owner string;var secdef bool
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef(oid),pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid=$1::regprocedure`,signature).Scan(&definition,&owner,&secdef);err!=nil{t.Fatal(err)}
 if owner!="photo_fn_owner"||!secdef{t.Fatal("signed receipt owner")}
 for _,required:=range []string{"app.tenant_id","v2_editor_authorized","v31_signing_policy_ready","adopted_result_id","canonical_result_id","validation_sha","produced_fence","SigningPolicy","signing:policy:","QualifiedExport","config_snapshot","policy_sha","graph_sealed","provenance:artifact-snapshot:1","provenance.sign:0","FOR SHARE"}{
  if !strings.Contains(definition,required){t.Fatal("signed receipt authority absent",required)}
 }
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("signed receipt role",role,allowed)}
 }
}
