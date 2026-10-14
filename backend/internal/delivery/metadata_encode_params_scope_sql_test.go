package delivery

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Installation regression only; does not replace a positive delivery lifecycle.
func TestIntegrationMetadataEncodeParamsScopesUseCleanIntermediate(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through01712 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
 for _,name:=range []string{"photo.v30_delivery_params_scope_initial(uuid,uuid,integer,text)","photo.v30_delivery_params_scope(uuid,uuid,integer,text)"} {
  var source string
  if err=tx.QueryRow(ctx,"SELECT prosrc FROM pg_proc WHERE oid=$1::regprocedure",name).Scan(&source);err!=nil{t.Fatal(err)}
  if !strings.Contains(source,"'icc_sha256',params->>'icc_sha256','metadata_policy','strip')") || !strings.Contains(source,"IF params IS DISTINCT FROM expected THEN RETURN NULL;END IF;") {
   t.Fatalf("%s lost clean encoder or exact parameter comparison",name)
  }
 }
}
