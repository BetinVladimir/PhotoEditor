package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
)
// Deferred until the implementation phase ends and migrations are applied to
// an isolated database. This tests SQL/Go restricted JCS byte parity only.
func TestIntegrationWarpCanvasSQLCanonicalParity(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal("database configuration failed")};defer db.Close()
 canvas:=review.Canvas{Width:123,Height:45,Orientation:1,SpaceID:"intermediate:crop-1",SourceSHA256:rawSHA([]byte("original"))}
 raw,err:=canonical.Marshal(canvas);if err!=nil{t.Fatal(err)}
 var actual *string
 if err:=db.QueryRow(ctx,`SELECT photo.v30_import_canvas_jcs($1::jsonb)`,string(raw)).Scan(&actual);err!=nil{t.Fatal(err)}
 if actual==nil||*actual!=string(raw){t.Fatal("SQL and Go canvas canonical bytes disagree")}
 for _,invalid:=range []string{`{}`,`{"width":1,"height":1,"orientation":1,"space_id":"x","source_sha256":"invalid"}`,`{"width":0,"height":1,"orientation":1,"space_id":"x","source_sha256":"invalid"}`} {
  actual=nil
  if err:=db.QueryRow(ctx,`SELECT photo.v30_import_canvas_jcs($1::jsonb)`,invalid).Scan(&actual);err!=nil{t.Fatal(err)}
  if actual!=nil{t.Fatal("invalid canvas serialized")}
 }
}
