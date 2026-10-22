package collection

import (
 "bytes"
 "encoding/json"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
)

// Actual Service calls under photo_api/RLS. This reuses the isolated database
// fixture, not mocked authorization or a fabricated accepted receipt.
func TestIntegrationDeletingCollectionRejectsFreshMutationsAndRetainsReceipts(t *testing.T){
 ctx,admin,service,who,tid,collection,_:=sectionAbandonFixture(t)
 service.Pepper=[]byte(strings.Repeat("p",32))
 var project,artifact,asset,version uuid.UUID
 if err:=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,collection).Scan(&project);err!=nil{t.Fatal(err)}
 sha:=strings.Repeat("a",64)
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,1,'image/jpeg','working_image','ready',now()) RETURNING id`,tid,uuid.NewString(),sha).Scan(&artifact);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,tid,project,artifact).Scan(&asset);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,tid,asset,artifact,sha).Scan(&version);err!=nil{t.Fatal(err)}
 itemReq:=ItemAdd{ExpectedRevision:1,AssetVersionID:version};itemKey:=uuid.NewString()
 itemReceipt,err:=service.AddItem(ctx,who,tid,collection,itemKey,itemReq);if err!=nil{t.Fatal(err)}
 var item Item;if err=json.Unmarshal(itemReceipt.Body,&item);err!=nil{t.Fatal(err)}
 sectionReq:=SectionCreate{ExpectedRevision:2,Name:"Ceremony",Position:0};sectionKey:=uuid.NewString()
 sectionReceipt,err:=service.CreateSection(ctx,who,tid,collection,sectionKey,sectionReq);if err!=nil{t.Fatal(err)}
 expires:=time.Now().UTC().Add(time.Hour)
 shareReq:=ShareCreate{Label:"Client",ExpiresAt:OptionalTime{Set:true,Value:&expires},Permissions:SharePermissions{View:true},Limits:ShareLimits{MaxFileBytes:1,MaxUploadBytesTotal:1},Moderation:"owner_review"}
 shareKey:=uuid.NewString();shareReceipt,err:=service.CreateShare(ctx,who,tid,collection,shareKey,shareReq);if err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.collections SET state='deleting' WHERE tenant_id=$1 AND id=$2`,tid,collection);err!=nil{t.Fatal(err)}
 snapshot:=func()[]byte{t.Helper();var raw []byte;err:=admin.QueryRow(ctx,`SELECT jsonb_build_object(
 'collection',(SELECT to_jsonb(c) FROM photo.collections c WHERE tenant_id=$1 AND id=$2),
 'items',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM photo.collection_items x WHERE tenant_id=$1),
 'sections',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM photo.sections x WHERE tenant_id=$1),
 'shares',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM photo.share_links x WHERE tenant_id=$1),
 'jobs',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM photo.deletion_jobs x WHERE tenant_id=$1),
 'audit',(SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1),
 'outbox',(SELECT count(*) FROM photo.outbox WHERE tenant_id=$1),
 'receipts',(SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1))`,tid,collection).Scan(&raw);if err!=nil{t.Fatal(err)};return raw}
 before:=snapshot();name:="Changed"
 cases:=[]struct{name string;call func()error}{
  {"update",func()error{_,e:=service.Update(ctx,who,tid,collection,4,Patch{Name:&name});return e}},
  {"archive",func()error{_,e:=service.Archive(ctx,who,tid,collection,4,DeleteRequest{ExpectedRevision:4,ConfirmResourceID:collection});return e}},
  {"add",func()error{_,e:=service.AddItem(ctx,who,tid,collection,uuid.NewString(),ItemAdd{ExpectedRevision:4,AssetVersionID:uuid.New()});return e}},
  {"moderate",func()error{_,e:=service.ModerateApplyItem(ctx,who,tid,collection,item.ID,ItemModeration{ExpectedRevision:4,Decision:"accept"});return e}},
  {"remove",func()error{return service.RemoveItem(ctx,who,tid,collection,item.ID,4)}},
  {"publish",func()error{_,e:=service.Publish(ctx,who,tid,collection,PublicationRequest{ExpectedRevision:4,ItemIDs:[]uuid.UUID{item.ID},Publish:true,PublishCollection:true});return e}},
  {"create share",func()error{_,e:=service.CreateShare(ctx,who,tid,collection,uuid.NewString(),shareReq);return e}},
  {"update share",func()error{_,e:=service.UpdateShare(ctx,who,tid,collection,shareReceipt.Share.ID,1,SharePatch{Label:&name});return e}},
  {"replace version",func()error{_,e:=service.ReplaceItemVersion(ctx,who,tid,collection,item.ID,ItemVersionReplace{ExpectedRevision:"4",ExpectedVersion:version,Version:uuid.New()});return e}},
 }
 for _,test:=range cases{t.Run(test.name,func(t *testing.T){e:=test.call();if e==nil||domain.AsError(e).Code!="REVISION_CONFLICT"{t.Fatalf("deleting collection admitted mutation: %v",e)};if !bytes.Equal(before,snapshot()){t.Fatal("rejected mutation changed collection, resources, jobs, audit/outbox or receipts")}})}
 // Receipt lookup precedes fresh state/CAS admission; no new resource appears.
 replay,err:=service.AddItem(ctx,who,tid,collection,itemKey,itemReq);if err!=nil||!replay.Replayed||!bytes.Equal(replay.Body,itemReceipt.Body){t.Fatalf("item accepted replay lost: %v",err)}
 replay,err=service.CreateSection(ctx,who,tid,collection,sectionKey,sectionReq);if err!=nil||!replay.Replayed||!bytes.Equal(replay.Body,sectionReceipt.Body){t.Fatalf("section accepted replay lost: %v",err)}
 shareReplay,err:=service.CreateShare(ctx,who,tid,collection,shareKey,shareReq);if err!=nil||!shareReplay.Replayed||shareReplay.Share.ID!=shareReceipt.Share.ID||shareReplay.Secret!=""{t.Fatalf("share replay lost or secret reissued: %v",err)}
 if !bytes.Equal(before,snapshot()){t.Fatal("accepted replay mutated domain or receipt cardinality")}
}

func TestIntegrationPublishedCollectionRemainsWritableAndArchiveIsNotRestore(t *testing.T){
 ctx,admin,service,who,tid,collection,_:=sectionAbandonFixture(t)
 if _,err:=admin.Exec(ctx,`UPDATE photo.collections SET state='published' WHERE tenant_id=$1 AND id=$2`,tid,collection);err!=nil{t.Fatal(err)}
 name:="Still editable";out,err:=service.Update(ctx,who,tid,collection,1,Patch{Name:&name});if err!=nil||out.State!="published"||out.Revision!="2"{t.Fatalf("published editing regressed: %+v %v",out,err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.collections SET state='archived' WHERE tenant_id=$1 AND id=$2`,tid,collection);err!=nil{t.Fatal(err)}
 state:="draft";if _,err=service.Update(ctx,who,tid,collection,2,Patch{State:&state});err==nil||domain.AsError(err).Code!="REVISION_CONFLICT"{t.Fatalf("archive unexpectedly exposed restore through Update: %v",err)}
}
