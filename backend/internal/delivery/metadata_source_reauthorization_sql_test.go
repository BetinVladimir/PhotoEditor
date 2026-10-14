package delivery

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationMetadataReauthorizationRejectsMissingSource(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through01715 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
 tid:=uuid.New();if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tid.String());err!=nil{t.Fatal(err)}
 var accepted bool
 if err=tx.QueryRow(ctx,`SELECT photo.v30_metadata_source_reauthorized($1,$2,$3,$4,'{}','{}','invalid','{}','{}','copyright',ARRAY[]::text[])`,tid,uuid.New(),uuid.New(),uuid.New()).Scan(&accepted);err!=nil||accepted{t.Fatal(accepted,err)}
}
