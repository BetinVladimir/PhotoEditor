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
// Value fixture only; requires isolated PostgreSQL through01707 and owner rights.
func TestIntegrationMetadataInventoryChildBindings(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
 parent:=uuid.NewString();binding:=map[string]any{"asset_version_id":uuid.NewString(),"canvas_hash":"canvas","source":map[string]any{"artifact_id":uuid.NewString(),"sha256":"working"}}
 metadata:=map[string]any{"artifact_id":uuid.NewString(),"sha256":"metadata"}
 for _,mode:=range []string{"exact","ordinal","parent","snapshot","source","publication","frozen","consent"}{
  entry:=map[string]any{"ordinal":0,"binding":binding,"metadata_source":metadata,"publication_sha256":"publication","frozen_sha256":"frozen"}
  inventory:=map[string]any{"schema_version":1,"parent_run_id":parent,"snapshot_digest":"snapshot","policy":"approved_exif","approved_fields":[]string{"author"},"items":[]any{entry}}
  request:=map[string]any{"ordinal":0,"parent_run_id":parent,"snapshot_digest":"snapshot","binding":binding,"config_jcs":`{"metadata_policy":"approved_exif"}`}
  config:=map[string]any{"v30_delivery_export":request,"v30_metadata_source":metadata,"v30_metadata_publication_sha256":"publication","v30_metadata_frozen_sha256":"frozen"}
  fields:=[]string{"author"}
  switch mode{case "ordinal":request["ordinal"]=1;case "parent":request["parent_run_id"]=uuid.NewString();case "snapshot":request["snapshot_digest"]="changed";case "source":config["v30_metadata_source"]=map[string]any{"artifact_id":uuid.NewString()};case "publication":config["v30_metadata_publication_sha256"]="changed";case "frozen":config["v30_metadata_frozen_sha256"]="changed";case "consent":fields=[]string{"gps"}}
  inventoryRaw,_:=json.Marshal(inventory);configRaw,_:=json.Marshal(config);var valid bool
  if err=tx.QueryRow(ctx,"SELECT photo.v30_metadata_inventory_child_valid($1::jsonb,$2::jsonb,$3::text[])",string(inventoryRaw),string(configRaw),fields).Scan(&valid);err!=nil{t.Fatal(err)}
  if valid!=(mode=="exact"){t.Fatal(mode,valid)}
 }
}
