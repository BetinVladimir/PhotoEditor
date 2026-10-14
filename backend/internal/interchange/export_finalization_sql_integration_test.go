package interchange
import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationExportFinalizationPrivilegesAndMissingCommittedProof(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01368 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_scheduler","photo_worker","photo_gateway","photo_portal"}{
  var finalizer,proof,predecessor bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.finalize_v2_run(uuid,uuid)','EXECUTE'),has_function_privilege($1,'photo.v30_interchange_export_committed(uuid,uuid)','EXECUTE'),has_function_privilege($1,'photo.finalize_v2_run_pre_export_finalization(uuid,uuid)','EXECUTE')`,role).Scan(&finalizer,&proof,&predecessor);err!=nil{t.Fatal(err)}
  if finalizer!=(role=="photo_scheduler")||proof||predecessor{t.Fatal("finalization bypass privilege",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid:=uuid.New();if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 var committed bool
 if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_export_committed($1,$2)`,tid,uuid.New()).Scan(&committed);err!=nil||committed{t.Fatal("missing run proved committed",err)}
 var definition string
 if err:=tx.QueryRow(ctx,`SELECT pg_get_functiondef('photo.finalize_v2_run(uuid,uuid)'::regprocedure)`).Scan(&definition);err!=nil{t.Fatal(err)}
 for _,gate:=range []string{"v30_interchange_export_committed","finalize_v2_run_pre_interchange","finalize_v2_run_pre_export_finalization"}{if !strings.Contains(definition,gate){t.Fatal("missing lifecycle gate",gate)}}
}
