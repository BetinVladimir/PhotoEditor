package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationExportJobProjectionPrivilegesAndAbsentScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01370 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_scheduler","photo_worker","photo_gateway","photo_portal"}{
  for _,function:=range []string{"get_v30_interchange","inspect_v30_interchange","get_v30_interchange_pre_export","inspect_v30_interchange_pre_export"}{
   var allowed bool
   signature:="photo."+function+"(uuid,uuid,uuid,uuid)"
   if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   expected:=role=="photo_api"&&(function=="get_v30_interchange"||function=="inspect_v30_interchange")
   if allowed!=expected{t.Fatal("projection privilege",role,function)}
  }
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid:=uuid.New();if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 for _,query:=range []string{`SELECT photo.get_v30_interchange($1,$2,$3,$4)`,`SELECT photo.inspect_v30_interchange($1,$2,$3,$4)`}{
  var raw []byte;if err:=tx.QueryRow(ctx,query,tid,uuid.New(),uuid.New(),uuid.New()).Scan(&raw);err!=nil||len(raw)!=0{t.Fatal("absent job leaked projection",err)}
 }
}
