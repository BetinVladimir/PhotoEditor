package interchange
import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Prepare checks actual columns/types under API role without creating fixtures,
// executing the lookup, opening objects or claiming positive export lifecycle.
func TestIntegrationExportSnapshotLookupPreparesAgainstCurrentSchema(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil{t.Fatal(err)}
 statement,err:=tx.Prepare(ctx,"export_snapshot_lookup_current",exportProposalSnapshotLookupSQL)
 if err!=nil{t.Fatal(err)}
 if len(statement.ParamOIDs)!=5||len(statement.Fields)!=8{t.Fatal("snapshot lookup wire shape changed")}
}
