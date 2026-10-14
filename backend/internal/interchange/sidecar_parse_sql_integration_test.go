package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestIntegrationSidecarParsePreservesSQLPrivilegesAndScope(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migrations01770/01771 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 functions:=[]struct{signature,role string}{
  {"photo.v30_interchange_parse_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)","photo_worker"},
  {"photo.save_v30_interchange_parse_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text)","photo_api"},
  {"photo.begin_v30_interchange_parse_run(uuid,uuid,uuid,text,text,text,text,text)","photo_api"},
  {"photo.v30_interchange_document_scope(uuid,uuid,uuid,text,text,uuid,text,text)",""},
 }
 for _,function:=range functions {
  var owner string
  if err:=db.QueryRow(ctx,`SELECT pg_get_userbyid(proowner) FROM pg_proc WHERE oid=$1::regprocedure`,function.signature).Scan(&owner);err!=nil{t.Fatal(err)}
  if owner!="photo_fn_owner"{t.Fatal("unexpected function owner",function.signature,owner)}
  for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
   var allowed bool
   if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,function.signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role==function.role){t.Fatal("unexpected execution grant",function.signature,role,allowed)}
  }
 }
 for _,format:=range []string{"photoeditor_bundle","xmp","captureone_sidecar"}{
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)}
  func(){defer tx.Rollback(ctx)
   tenant,actor,session,project:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
   if _,err:=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{t.Fatal(err)}
   if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{t.Fatal(err)}
   params:=ParseParams{SchemaVersion:1,Request:ImportRequest{ProjectID:project,Direction:"import",Format:format,
    Bundle:v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},MaxExpandedBytes:1024,MaxFiles:10}
   raw,err:=CompileParseParams(params);if err!=nil{t.Fatal(err)}
   sha,err:=canonical.Hash(params);if err!=nil{t.Fatal(err)}
   var accepted bool
   err=tx.QueryRow(ctx,`SELECT photo.save_v30_interchange_parse_proof($1,$2,$3,$4,$5,$6,$7,$8,$9)`,tenant,actor,session,project,uuid.New(),uuid.New(),uuid.New(),sha,string(raw)).Scan(&accepted)
   if err!=nil||accepted{t.Fatal("absent tenant admitted",format,err)}
   var resource []byte
   err=tx.QueryRow(ctx,`SELECT photo.get_v30_interchange($1,$2,$3,$4)`,tenant,actor,session,uuid.New()).Scan(&resource)
   if err!=nil||len(resource)!=0{t.Fatal("absent job projection leaked",format,err)}
  }()
 }
}

