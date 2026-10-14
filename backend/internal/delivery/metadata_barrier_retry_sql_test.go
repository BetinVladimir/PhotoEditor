package delivery

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Installation assertion; full unknown-commit lifecycle coverage remains separate.
func TestIntegrationMetadataBarrierRetryRetainsRasterEvidence(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database migrated through01714 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var source string
 err=db.QueryRow(ctx,`SELECT prosrc FROM pg_proc WHERE oid='photo.begin_v30_metadata_barrier(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)'::regprocedure`).Scan(&source)
 if err!=nil{t.Fatal(err)}
 for _,required:=range []string{"raster.bytes=p_bytes","raster.state='ready'","raster.verified_at IS NOT NULL","raster.output_role='encoded_image'","raster.expires_at>clock_timestamp()","existing.v30_metadata_encoded_jcs=p_encoded"}{
  if !strings.Contains(source,required){t.Fatalf("missing replay evidence predicate: %s",required)}
 }
}
