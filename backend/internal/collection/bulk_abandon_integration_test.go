package collection

import (
 "errors"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

// Source-authored only. Actual API-role transactions and receipt locks, using
// the disposable migrated database fixture; no production checks are executed.
func TestIntegrationBulkAbandonUnknownSnapshotAndPendingRetention(t *testing.T){
 ctx,admin,service,who,tid,id,other:=sectionAbandonFixture(t)
 request:=BulkAbandonRequest{SelectionKey:uuid.NewString(),Selection:SelectionRequest{CollectionRevision:"1",Scope:"selected",ItemIDs:[]uuid.UUID{uuid.New()}},Action:"rating",Params:[]byte(`{"rating":5}`)}
 key:=uuid.NewString()
 // Simulate an unsealed pre-existing Selection receipt (not a completed
 // snapshot). Its short expiry must not survive completion as a tombstone.
 normalized,_,err:=normalizeSelection(request.Selection);if err!=nil{t.Fatal(err)};hash,err:=canonical.Hash(normalized);if err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,'Selection.Create',$3,$4,now()+interval '1 hour')`,tid,who.UserID,request.SelectionKey,hash);err!=nil{t.Fatal(err)}
 result,err:=service.AbandonBulk(ctx,who,tid,id,key,request);if err!=nil||!isBulkAbandoned(result,id,key){t.Fatalf("abandon: %+v %v",result,err)}
 _,err=service.CreateSelection(ctx,who,tid,id,request.SelectionKey,request.Selection);var failure *domain.Error
 if !errors.As(err,&failure)||failure.Code!="SELECTION_ABANDONED"{t.Fatalf("late selection: %v",err)}
 // Even a different tab which sends a syntactically valid snapshot ID cannot
 // cross the nil-only tombstone fingerprint and create a run.
 _,err=service.StartBulk(ctx,who,tid,id,key,BulkRequest{ExpectedRevision:1,SelectionSnapshotID:uuid.New(),Action:request.Action,Params:request.Params})
 if !errors.As(err,&failure)||failure.Code!="IDEMPOTENCY_CONFLICT"{t.Fatalf("late start: %v",err)}
 if _,err=service.AbandonBulk(ctx,who,tid,other,key,request);err==nil{t.Fatal("tombstone crossed collection parent")}
 var receipts,permanent,runs int
 if err=admin.QueryRow(ctx,`SELECT count(*),count(*) FILTER(WHERE extract(year from expires_at)=9999 AND response_status=200 AND resource_id IS NULL AND parent_binding=$4) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND ((route='Selection.Create' AND key=$3) OR (route='Bulk.Start' AND key=$5))`,tid,who.UserID,request.SelectionKey,"collection:"+id.String(),key).Scan(&receipts,&permanent);err!=nil||receipts!=2||permanent!=2{t.Fatalf("receipts %d permanent %d: %v",receipts,permanent,err)}
 if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.bulk_operations WHERE tenant_id=$1 AND collection_id=$2`,tid,id).Scan(&runs);err!=nil||runs!=0{t.Fatalf("unexpected admission %d: %v",runs,err)}
}

func TestIntegrationBulkAcceptedBeforeAbandonReturnsBothExactReceipts(t *testing.T){
 ctx,admin,service,who,tid,id,_:=sectionAbandonFixture(t)
 digest:="e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
 var project,artifact,asset,version,item uuid.UUID
 if err:=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,id).Scan(&project);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES($1,'photo',$2,'v1',$3,0,'image/jpeg','working_image') RETURNING id`,tid,uuid.NewString(),digest).Scan(&artifact);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,tid,project,artifact).Scan(&asset);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,tid,asset,artifact,digest).Scan(&version);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position) VALUES($1,$2,$3,$4,0) RETURNING id`,tid,id,asset,version).Scan(&item);err!=nil{t.Fatal(err)}
 var revision string;if err:=admin.QueryRow(ctx,`SELECT revision::text FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,id).Scan(&revision);err!=nil{t.Fatal(err)}
 selection:=SelectionRequest{CollectionRevision:revision,Scope:"selected",ItemIDs:[]uuid.UUID{item}};selectionKey,key:=uuid.NewString(),uuid.NewString()
 selected,err:=service.CreateSelection(ctx,who,tid,id,selectionKey,selection);if err!=nil{t.Fatal(err)}
 var ref SnapshotRef;if err=json.Unmarshal(selected.Body,&ref);err!=nil{t.Fatal(err)}
 _,number,err:=normalizeSelection(selection);if err!=nil{t.Fatal(err)}
 req:=BulkRequest{ExpectedRevision:number,SelectionSnapshotID:ref.ID,Action:"rating",Params:[]byte(`{"rating":5}`)}
 accepted,err:=service.StartBulk(ctx,who,tid,id,key,req);if err!=nil{t.Fatal(err)}
 resolved,err:=service.AbandonBulk(ctx,who,tid,id,key,BulkAbandonRequest{SelectionKey:selectionKey,Selection:selection,Action:req.Action,Params:req.Params});if err!=nil||resolved.Status!=202{t.Fatalf("accepted resolution: %+v %v",resolved,err)}
 var response struct{Status string `json:"status"`;Snapshot SnapshotRef `json:"snapshot"`;Ticket BulkAccepted `json:"ticket"`};if err=json.Unmarshal(resolved.Body,&response);err!=nil{t.Fatal(err)}
 var ticket BulkAccepted;if err=json.Unmarshal(accepted.Body,&ticket);err!=nil{t.Fatal(err)}
 if response.Status!="accepted"||response.Snapshot!=ref||response.Ticket!=ticket{t.Fatalf("receipt replacement: %+v",response)}
 // Completed admission remains a single operation and normal start replays its
 // original raw202 body, not the abandon endpoint's resolution envelope.
 replay,err:=service.StartBulk(ctx,who,tid,id,key,req);if err!=nil||string(replay.Body)!=string(accepted.Body){t.Fatalf("start receipt changed: %+v %v",replay,err)}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.bulk_operations WHERE tenant_id=$1 AND collection_id=$2`,tid,id).Scan(&count);err!=nil||count!=1{t.Fatalf("duplicate operations %d: %v",count,err)}
 // Two independent API transactions race normal admission against abandonment
 // against a committed selection. Either lock winner is legal; disagreement,
 // duplicate children, missing permanent refusal or deadlock is not.
 raceSelectionKey,raceKey:=uuid.NewString(),uuid.NewString()
 raceSelection,err:=service.CreateSelection(ctx,who,tid,id,raceSelectionKey,selection);if err!=nil{t.Fatal(err)}
 var raceRef SnapshotRef;if err=json.Unmarshal(raceSelection.Body,&raceRef);err!=nil{t.Fatal(err)}
 raceReq:=req;raceReq.SelectionSnapshotID=raceRef.ID
 type result struct{out idempotency.Response;err error};starts,resolutions:=make(chan result,1),make(chan result,1);begin:=make(chan struct{})
 go func(){<-begin;out,e:=service.StartBulk(ctx,who,tid,id,raceKey,raceReq);starts<-result{out,e}}()
 go func(){<-begin;out,e:=service.AbandonBulk(ctx,who,tid,id,raceKey,BulkAbandonRequest{SelectionKey:raceSelectionKey,Selection:selection,Action:req.Action,Params:req.Params});resolutions<-result{out,e}}()
 close(begin)
 var startResult,abandonResult result
 select{case startResult=<-starts:case <-ctx.Done():t.Fatal("normal admission blocked by receipt lock cycle")}
 select{case abandonResult=<-resolutions:case <-ctx.Done():t.Fatal("abandonment blocked by receipt lock cycle")}
 if abandonResult.err!=nil{t.Fatal(abandonResult.err)}
 if abandonResult.out.Status==202{
  if startResult.err!=nil||startResult.out.Status!=202{t.Fatalf("accepted winner disagrees: %v",startResult.err)}
  var resolved struct{Ticket BulkAccepted `json:"ticket"`};if err=json.Unmarshal(abandonResult.out.Body,&resolved);err!=nil{t.Fatal(err)}
  var direct BulkAccepted;if err=json.Unmarshal(startResult.out.Body,&direct);err!=nil||resolved.Ticket!=direct{t.Fatalf("different accepted winner: %+v %+v %v",resolved.Ticket,direct,err)}
 }else{
  var refused *domain.Error;if !isBulkAbandoned(abandonResult.out,id,raceKey)||!errors.As(startResult.err,&refused)||refused.Code!="BULK_ABANDONED"{t.Fatalf("abandon winner disagrees: %+v %v",abandonResult.out,startResult.err)}
 }
 var children int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.bulk_operations WHERE tenant_id=$1 AND collection_id=$2 AND snapshot_id=$3`,tid,id,raceRef.ID).Scan(&children);err!=nil{t.Fatal(err)}
 expected:=0;if abandonResult.out.Status==202{expected=1};if children!=expected{t.Fatalf("race created %d operations; wanted %d",children,expected)}

}
