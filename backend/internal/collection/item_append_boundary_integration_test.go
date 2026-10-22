package collection

import (
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
)

// Relational fixture only: actual API-role Service/receipt/CAS, no image IO.
func TestIntegrationItemAppendCannotProduceUnsafePosition(t *testing.T){
 ctx,admin,service,who,tid,collection,_:=sectionAbandonFixture(t)
 var project uuid.UUID
 if err:=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,collection).Scan(&project);err!=nil{t.Fatal(err)}
 versions:=make([]uuid.UUID,2)
 for index:=range versions{
  var artifact,asset uuid.UUID;sha:=strings.Repeat(string(rune('a'+index)),64)
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,1,'image/jpeg','working_image','ready',now()) RETURNING id`,tid,uuid.NewString(),sha).Scan(&artifact);err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,tid,project,artifact).Scan(&asset);err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,tid,asset,artifact,sha).Scan(&versions[index]);err!=nil{t.Fatal(err)}
 }
 last:=int64(9007199254740991);key:=uuid.NewString()
 receipt,err:=service.AddItem(ctx,who,tid,collection,key,ItemAdd{ExpectedRevision:1,AssetVersionID:versions[0],Position:&last});if err!=nil{t.Fatal(err)}
 var accepted Item;if err=json.Unmarshal(receipt.Body,&accepted);err!=nil||accepted.Position!=last{t.Fatal(err,accepted.Position)}
 _,err=service.AddItem(ctx,who,tid,collection,uuid.NewString(),ItemAdd{ExpectedRevision:2,AssetVersionID:versions[1]})
 if e:=domain.AsError(err);e.Code!="VALIDATION_FAILED"||e.HTTPStatus!=422{t.Fatalf("unsafe append accepted: %v",err)}
 var count int;var revision int64
 if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.collection_items WHERE tenant_id=$1 AND collection_id=$2`,tid,collection).Scan(&count);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,collection).Scan(&revision);err!=nil{t.Fatal(err)}
 if count!=1||revision!=2{t.Fatal("refused append mutated collection",count,revision)}
 replay,err:=service.AddItem(ctx,who,tid,collection,key,ItemAdd{ExpectedRevision:1,AssetVersionID:versions[0],Position:&last});if err!=nil||!replay.Replayed||string(replay.Body)!=string(receipt.Body){t.Fatal("accepted receipt lost",err)}
}
