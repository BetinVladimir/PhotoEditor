package interchange
import (
 "context"
 "os"
 "testing"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationExportDedicatedAdoptionPrivilegesAndNoAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01367 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.adopt_v30_interchange_export(uuid,uuid,uuid,text,text,text,text)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("unexpected export adoption privilege",role)}
 }
 var definition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.adopt_v30_interchange_export(uuid,uuid,uuid,text,text,text,text)'::regprocedure)`).Scan(&definition);err!=nil{t.Fatal(err)}
 if !strings.Contains(definition,"revision=revision+1")||!strings.Contains(definition,"RUN_REVISION_EXHAUSTED"){t.Fatal("missing adoption revision guard")}
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid:=uuid.New();if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 var adopted bool
 if err:=tx.QueryRow(ctx,`SELECT photo.adopt_v30_interchange_export($1,$2,$3,$4,$5,$6,$7)`,tid,uuid.New(),uuid.New(),"missing-ledger",rawSHA([]byte("request")),"{}",rawSHA([]byte("{}"))).Scan(&adopted);err!=nil||adopted{t.Fatal("missing authority adopted",err)}
 for _,query:=range []string{`SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=$1`,`SELECT count(*) FROM photo.inbox WHERE tenant_id=$1`,`SELECT count(*) FROM photo.outbox WHERE tenant_id=$1`,`SELECT count(*) FROM photo.v30_interchange_export_evidence WHERE tenant_id=$1`}{
  var count int;if err:=tx.QueryRow(ctx,query,tid).Scan(&count);err!=nil||count!=0{t.Fatal("refusal wrote adoption state",err)}
 }
}
