package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
)
func TestExportOutputCatalogRequiresDatabaseAndOutputContract(t *testing.T){
 for _,contract:=range []string{"InterchangeResult","MappingReport","MappingShard","InterchangeArchive","WorkingImage","Params",""}{
  if _,err:=(SQLExportOutputCatalog{}).LookupExportOutput(context.Background(),v2input.Entry{Contract:contract});err==nil{t.Fatal("missing authority accepted",contract)}
 }
}
func TestIntegrationExportOutputCatalogPrivilegesAndAbsentAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01362 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.v30_interchange_export_output(uuid,uuid,uuid,text,text,uuid,text,text)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("unexpected output catalog privilege",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid:=uuid.New();if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 for _,contract:=range []string{"InterchangeResult","MappingReport","MappingShard","InterchangeArchive","WorkingImage"}{
  var bucket string
  err=tx.QueryRow(ctx,`SELECT bucket FROM photo.v30_interchange_export_output($1,$2,$3,$4,$5,$6,$7,$8)`,tid,uuid.New(),uuid.New(),"missing-ledger",rawSHA([]byte("request")),uuid.New(),rawSHA([]byte("body")),contract).Scan(&bucket)
  if err!=pgx.ErrNoRows{t.Fatal("absent authority returned output metadata",contract,err)}
 }
}

func TestIntegrationExportEvidencePrivilegesAndAbsentScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01364 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed,insertAllowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.save_v30_interchange_export_evidence(uuid,uuid,uuid,text,text,text,text)','EXECUTE'),has_table_privilege($1,'photo.v30_interchange_export_evidence','INSERT')`,role).Scan(&allowed,&insertAllowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api")||insertAllowed{t.Fatal("unexpected evidence privilege",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid:=uuid.New();if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 var saved bool
 if err:=tx.QueryRow(ctx,`SELECT photo.save_v30_interchange_export_evidence($1,$2,$3,$4,$5,$6,$7)`,tid,uuid.New(),uuid.New(),"missing-ledger",rawSHA([]byte("request")),"{}",rawSHA([]byte("{}"))).Scan(&saved);err!=nil||saved{t.Fatal("absent evidence accepted",err)}
 var count int
 if err:=tx.QueryRow(ctx,`SELECT count(*) FROM photo.v30_interchange_export_evidence WHERE tenant_id=$1`,tid).Scan(&count);err!=nil||count!=0{t.Fatal("refusal wrote evidence",err)}
}
