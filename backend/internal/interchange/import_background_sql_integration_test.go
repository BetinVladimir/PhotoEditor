package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationBackgroundPreparedPrivilegesAndAbsentScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migrations 01376/01377 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signatures:=[]struct{name string;scheduler bool}{
  {"photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text)",false},
  {"photo.v30_interchange_prepared_scope_without_background(uuid,uuid,text,uuid,text,text)",false},
  {"photo.reserve_v30_interchange_prepared(uuid,uuid,text,uuid,text,text,text)",true},
  {"photo.complete_v30_interchange_prepared(uuid,uuid,text,uuid,text,text,text,uuid,text)",true},
  {"photo.v30_interchange_prepared_document(uuid,uuid,text,uuid,jsonb,uuid,text,text)",true},
 }
 for _,signature:=range signatures{for _,role:=range []string{"photo_api","photo_scheduler","photo_worker","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature.name).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler"&&signature.scheduler){t.Fatal("unexpected privilege",role,signature.name)}
 }}
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid:=uuid.New();if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 for _,body:=range []string{`{}`,`null`,`{"background_occurrence":null}`,`{"background_occurrence":{},"bytes":536870913}`}{
  var refused bool
  if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_prepared_scope($1,$2,'{}',$3,'StyleBackgroundAsset',$4) IS NULL`,tid,uuid.New(),uuid.New(),body).Scan(&refused);err!=nil||!refused{t.Fatal("accepted malformed/absent authority",err)}
 }
 var count int
 if err:=tx.QueryRow(ctx,`SELECT count(*) FROM photo.v30_interchange_prepared_document($1,$2,'{}',$3,'{}'::jsonb,$4,$5,'StyleBackgroundAsset')`,tid,uuid.New(),uuid.New(),uuid.New(),"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&count);err!=nil||count!=0{t.Fatal("absent background returned",err)}
}
