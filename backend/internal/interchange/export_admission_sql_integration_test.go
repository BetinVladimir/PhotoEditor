package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationExportWorkerAdmissionPrivilegesAndAbsentLease(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01361 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.v30_interchange_export_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_worker"){t.Fatal("unexpected admission privilege",role)}
 }
 for _,attempt:=range []int{0,1}{
  var receipt []byte
  if err:=db.QueryRow(ctx,`SELECT photo.v30_interchange_export_ready($1,$2,$3,$4,1,$5,$6,$7,$8,$9)`,uuid.New(),uuid.New(),uuid.New(),attempt,uuid.New(),uuid.New(),rawSHA([]byte(`{}`)),`{}`,rawSHA([]byte("closure"))).Scan(&receipt);err!=nil{t.Fatal(err)}
  if len(receipt)!=0{t.Fatal("absent lease obtained export authority")}
 }
}
