package provenance

import (
 "context"
 "os"
 "strings"
 "testing"

 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationSigningAdoptionReceiptInstalled(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migrations through 01811 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,column:=range []string{"adopted_task_id","adopted_result_id","validation_sha"}{
  var present bool
  if err:=db.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='photo.provenance_manifests'::regclass AND attname=$1 AND NOT attisdropped)`,column).Scan(&present);err!=nil||!present{t.Fatal("signing receipt column absent",column,err)}
 }
 var definition,owner string;var secdef bool
 signature:="photo.adopt_v31_signed(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text)"
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef(oid),pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid=$1::regprocedure`,signature).Scan(&definition,&owner,&secdef);err!=nil{t.Fatal(err)}
 if owner!="photo_fn_owner"||!secdef{t.Fatal("signing adoption owner")}
 for _,field:=range []string{"adopted_task_id","adopted_result_id","validation_sha","p_task,p_result,p_validation_sha","adopt_v2_plan_pre_v31_signer"}{
  if !strings.Contains(definition,field){t.Fatal("signing adoption receipt absent",field)}
 }
 var trigger string
 if err:=db.QueryRow(ctx,`SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgrelid='photo.provenance_manifests'::regclass AND tgname='immutable_content' AND NOT tgisinternal`).Scan(&trigger);err!=nil{t.Fatal(err)}
 for _,field:=range []string{"adopted_task_id","adopted_result_id","validation_sha"}{if !strings.Contains(trigger,field){t.Fatal("receipt mutable",field)}}
}
