package collection

import (
 "strings"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/pagination"
)

// Source-authored application-role fixture: filters precede pagination and
// signed cursors cannot be borrowed across distinct gallery predicates.
func TestIntegrationGalleryFiltersBeforePagination(t *testing.T){
 ctx,admin,service,who,tid,cid,_:=sectionAbandonFixture(t)
 service.Cursors=pagination.Codec{Key:[]byte(strings.Repeat("g",32))}
 var project uuid.UUID;if err:=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,cid).Scan(&project);err!=nil{t.Fatal(err)}
 insert:=func(position int,review,tag string)uuid.UUID{
  t.Helper();var artifact,asset,version,item uuid.UUID;digest:=strings.Repeat("a",64)
  capture:=map[string]any{"source":map[string]any{"sha256":digest}}
  if position==0{capture["captured_at"]="2026-10-17T12:00:00Z"}else if position==1{capture["captured_at"]="2026-10-17T10:00:00Z"}else{capture["captured_local"]="2026-10-17T09:00:00"}
  metadata,_:=json.Marshal(map[string]any{"ingest":capture})
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,100,'image/jpeg','working_image','ready',now()) RETURNING id`,tid,uuid.NewString(),digest).Scan(&artifact);err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,$4,'verified','{}') RETURNING id`,tid,project,artifact,metadata).Scan(&asset);err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}',$5) RETURNING id`,tid,asset,artifact,digest,review).Scan(&version);err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,visibility,moderation_state,rating,flag,tags) VALUES($1,$2,$3,$4,$5,'hidden',$6,5,'pick',jsonb_build_array($7::text)) RETURNING id`,tid,cid,asset,version,position,review,tag).Scan(&item);err!=nil{t.Fatal(err)}
  uploadID,reservation:=uuid.New(),uuid.New();var snapshot,run uuid.UUID
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id`,tid,who.UserID,digest).Scan(&snapshot);err!=nil{t.Fatal(err)}
  if _,err:=admin.Exec(ctx,`INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload) VALUES($1,$2,0,$3,$4,'{}')`,tid,snapshot,artifact,digest);err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'ingest','completed',$2,'{}',$3,$4,true) RETURNING id`,tid,snapshot,digest,who.UserID).Scan(&run);err!=nil{t.Fatal(err)}
  if _,err:=admin.Exec(ctx,`INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at) VALUES($1,$2,'user',$3,'upload',$4,100,1,now()+interval '1 day')`,reservation,tid,who.UserID,uploadID);err!=nil{t.Fatal(err)}
  filename:=[]string{"zeta.jpg","alpha.jpg","middle.jpg"}[position]
  if _,err:=admin.Exec(ctx,`INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,principal_type,principal_id,expected_size,actual_size,filename,multipart_id,object_key,state,expires_at,reservation_id,staging_candidate_id,ingest_snapshot_id,ingest_run_id,staging_artifact_id,verified_version_id,media_type) VALUES($1,$2,$3,$4,'user',$5,100,100,$6,'fixture-multipart',$7,'verified',now()+interval '1 day',$8,$9,$10,$11,$9,$12,'image/jpeg')`,uploadID,tid,project,cid,who.UserID,filename,uuid.NewString(),reservation,artifact,snapshot,run,version);err!=nil{t.Fatal(err)}
  if _,err:=admin.Exec(ctx,`INSERT INTO photo.ingest_upload_bindings(tenant_id,run_id,ordinal,upload_id,source_id,source_sha,project_id,collection_id,principal_type,principal_id,asset_id,version_id,item_id) VALUES($1,$2,0,$3,$4,$5,$6,$7,'user',$8,$9,$10,$11)`,tid,run,uploadID,artifact,digest,project,cid,who.UserID,asset,version,item);err!=nil{t.Fatal(err)}
  return item
 }
 later:=insert(0,"accepted","other");first:=insert(1,"pending_review","Wedding");second:=insert(2,"pending_review","Wedding")
 filter:=ItemGalleryFilter{Search:" wedding ",Review:"pending_review",Visibility:"hidden"}
 page,err:=service.ListItems(ctx,who,tid,cid,1,"","","pick","","5",filter)
 if err!=nil||len(page.Items)!=1||page.Items[0].ID!=first||page.NextCursor==nil{t.Fatalf("filtered first page: %+v %v",page,err)}
 next,err:=service.ListItems(ctx,who,tid,cid,1,*page.NextCursor,"","pick","","5",filter)
 if err!=nil||len(next.Items)!=1||next.Items[0].ID!=second||next.NextCursor!=nil{t.Fatalf("filtered next page: %+v %v",next,err)}
 if _,err=service.ListItems(ctx,who,tid,cid,1,*page.NextCursor,"","pick","","5",ItemGalleryFilter{Search:"wedding",Review:"accepted",Visibility:"hidden"});err==nil{t.Fatal("cursor crossed review predicate")}
 all,err:=service.ListItems(ctx,who,tid,cid,100,"","","","","")
 if err!=nil||len(all.Items)!=3{t.Fatalf("gallery filter changed full selection: %+v %v",all,err)}
 named,err:=service.ListItems(ctx,who,tid,cid,100,"","","","","",ItemGalleryFilter{Search:" ALPHA.JPG "})
 if err!=nil||len(named.Items)!=1||named.Items[0].ID!=first{t.Fatalf("bound original filename search: %+v %v",named,err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.collections SET sort_mode='filename',revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,cid);err!=nil{t.Fatal(err)}
 filenames,err:=service.ListItems(ctx,who,tid,cid,100,"","","","","")
 if err!=nil||len(filenames.Items)!=3||filenames.Items[0].ID!=first||filenames.Items[1].ID!=second||filenames.Items[2].ID!=later{t.Fatalf("bound filename order: %+v %v",filenames,err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.collections SET sort_mode='captured_at',revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,cid);err!=nil{t.Fatal(err)}
 sorted,err:=service.ListItems(ctx,who,tid,cid,100,"","","","","")
 if err!=nil||len(sorted.Items)!=3||sorted.Items[0].ID!=first||sorted.Items[1].ID!=later||sorted.Items[2].ID!=second{t.Fatalf("qualified capture sort / floating last: %+v %v",sorted,err)}
 if sorted.Items[0].Position!=1{t.Fatal("effective ordering overwrote manual position")}
 if _,err=service.ListItems(ctx,who,tid,cid,1,*page.NextCursor,"","pick","","5",filter);err==nil{t.Fatal("old revision/order cursor accepted")}
}
