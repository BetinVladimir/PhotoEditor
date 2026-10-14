package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationExportDraftPrivilegesAndAbsentLedger(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database with migration01358 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.begin_v30_interchange_export_run(uuid,uuid,uuid,text,text,text)"
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatalf("unexpected draft privilege: %s",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid:=uuid.New()
 if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 actor:=uuid.New();rid:=uuid.NewSHA1(tid,[]byte("interchange:create:"+actor.String()+":absent-export-ledger"));var before,after int
 if err:=tx.QueryRow(ctx,`SELECT count(*) FROM photo.runs WHERE id=$1`,rid).Scan(&before);err!=nil{t.Fatal(err)}
 var accepted bool
 if err:=tx.QueryRow(ctx,`SELECT photo.begin_v30_interchange_export_run($1,$2,$3,$4,$5,$6)`,tid,actor,uuid.New(),"absent-export-ledger",rawSHA([]byte("request")),"{}").Scan(&accepted);err!=nil{t.Fatal(err)}
 if accepted{t.Fatal("absent ledger authorized draft")}
 if err:=tx.QueryRow(ctx,`SELECT count(*) FROM photo.runs WHERE id=$1`,rid).Scan(&after);err!=nil{t.Fatal(err)}
 if before!=after{t.Fatal("absent authority changed run")}
}
