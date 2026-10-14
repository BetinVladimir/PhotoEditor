package delivery

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Constructor fixture only. Non-strip success needs a full pinned parent and
// ready source lifecycle fixture; these refusal checks cannot prove admission.
func TestIntegrationMetadataChildConfigRequiresPinnedParent(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through01709 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"strip","copyright","approved_exif","unknown"}{
  config,_:=json.Marshal(map[string]any{"metadata_policy":mode})
  request,_:=json.Marshal(map[string]any{"child_run_id":uuid.NewString(),"parent_run_id":uuid.NewString(),"ordinal":0,"config_jcs":string(config)})
  var accepted bool
  err=tx.QueryRow(ctx,"SELECT photo.v32_delivery_child_config($1,$2,$3::jsonb,$4) IS NOT NULL",uuid.New(),uuid.New(),string(request),uuid.New()).Scan(&accepted)
  if err!=nil{t.Fatal(err)};if accepted!=(mode=="strip"){t.Fatal(mode,accepted)}
 }
}
