package delivery

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Requires an isolated database migrated through 01705 and fixture owner rights.
// Covers value authority only, not parent/child admission or physical metadata.
func TestIntegrationMetadataParentConsentValues(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.Background())
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{t.Fatal(err)}
 for _,c:=range []struct{policy,mode string;fields []string;want bool}{
  {`{"metadata_policy":"strip"}`,"strip",[]string{},true},
  {`{"metadata_policy":"copyright"}`,"copyright",[]string{},true},
  {`{"metadata_policy":"approved_exif","approved_fields":["author","gps"]}`,"approved_exif",[]string{"gps","author"},true},
  {`{"metadata_policy":"approved_exif"}`,"approved_exif",[]string{"gps"},false},
  {`{"metadata_policy":"approved_exif","approved_fields":null}`,"approved_exif",[]string{},false},
  {`{"metadata_policy":"approved_exif","approved_fields":["gps"]}`,"approved_exif",[]string{"gps","gps"},false},
  {`{"metadata_policy":"strip","approved_fields":["gps"]}`,"strip",[]string{"gps"},false},
  {`{"metadata_policy":"copyright"}`,"approved_exif",[]string{},false},
 }{
  var valid bool
  if err=tx.QueryRow(ctx,"SELECT photo.v30_metadata_policy_approval_valid($1::jsonb,$2,$3::text[])",c.policy,c.mode,c.fields).Scan(&valid);err!=nil{t.Fatal(err)}
  if valid!=c.want{t.Fatalf("policy=%s fields=%v got=%v",c.policy,c.fields,valid)}
 }
}
