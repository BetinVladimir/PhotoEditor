package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationExportStoragePrivilegesAndAbsentScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database with migration01357 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal("database configuration failed")};defer db.Close()
 for _,signature:=range []string{"photo.reserve_v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text,text)","photo.complete_v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text,text,uuid,text)"}{
  for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
   var allowed bool
   if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role=="photo_api"){t.Fatalf("unexpected %s privilege for %s",signature,role)}
  }
 }
 var outcome string;var artifact,token *uuid.UUID;var ready bool
 if err:=db.QueryRow(ctx,`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_interchange_export_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`,uuid.New(),uuid.New(),uuid.New(),"missing-key",rawSHA([]byte("request")),uuid.New(),"task_params","{}","bucket").Scan(&outcome,&artifact,&token,&ready);err!=nil{t.Fatal(err)}
 if outcome!="forbidden"||artifact!=nil||token!=nil||ready{t.Fatal("absent scope obtained storage authority")}
}
