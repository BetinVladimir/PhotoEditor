package delivery

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Missing-parent refusal only; positive full lifecycle is a separate obligation.
func TestIntegrationMetadataPrepareRequiresPersistedInventory(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through01713 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.v30_metadata_parent_inventory_ready($1,$2,$3,$4,'{"policy":{"metadata_policy":"copyright"}}'::jsonb,'{}')`,uuid.New(),uuid.New(),uuid.New(),uuid.New()).Scan(&accepted)
 if err!=nil||accepted{t.Fatal(accepted,err)}
}

// A tenant mismatch must fail before any persisted inventory can authorize it.
func TestIntegrationMetadataPrepareRejectsForeignTenantContext(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through01713 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",uuid.New().String());err!=nil{t.Fatal(err)}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.v30_metadata_parent_inventory_ready($1,$2,$3,$4,'{}'::jsonb,'{}')`,uuid.New(),uuid.New(),uuid.New(),uuid.New()).Scan(&accepted)
 if err!=nil||accepted{t.Fatal(accepted,err)}
}
