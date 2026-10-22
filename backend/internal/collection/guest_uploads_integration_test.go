package collection

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/upload"
)

// Authoring only: these calls use the actual photo_api pool, signed cursors and
// tenant RLS in an isolated fully migrated PostgreSQL, without mocked queries.
func TestIntegrationGuestUploadsScopePagingAndSafeMetadata(t *testing.T) {
	ctx,admin,service,who,tid,collection,other:=sectionAbandonFixture(t)
	service.Pepper=[]byte(strings.Repeat("p",32))
	service.Cursors=pagination.Codec{Key:[]byte(strings.Repeat("c",32))}
	shareRequest:=ShareCreate{
		Label:"Guest upload",Permissions:SharePermissions{Upload:true},
		Limits:ShareLimits{MaxUploadFiles:10,MaxUploadBytesTotal:1000,MaxFileBytes:100,AllowedFormats:[]string{"jpeg"}},Moderation:"owner_review",
	}
	share,err:=service.CreateShare(ctx,who,tid,collection,uuid.NewString(),shareRequest)
	if err!=nil{t.Fatal(err)}
	otherShare,err:=service.CreateShare(ctx,who,tid,other,uuid.NewString(),shareRequest)
	if err!=nil{t.Fatal(err)}
	var project uuid.UUID
	if err=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,collection).Scan(&project);err!=nil{t.Fatal(err)}
	stamp:=time.Date(2026,10,17,12,0,0,0,time.UTC)
	insert:=func(cid uuid.UUID,principal string,shareID *uuid.UUID,ordinal int)uuid.UUID{
		t.Helper();id,guest,reservation:=uuid.New(),uuid.New(),uuid.New()
		if _,e:=admin.Exec(ctx,`INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,share_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES($1,$2,$3,$4,$5,'upload',$6,100,1,now()+interval '1 day')`,reservation,tid,principal,guest,shareID,id);e!=nil{t.Fatal(e)}
		if _,e:=admin.Exec(ctx,`INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,principal_type,principal_id,share_id,expected_size,filename,multipart_id,object_key,state,expires_at,reservation_id,created_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,100,$8,'private-multipart','private-object-key','uploading',now()+interval '1 day',$9,$10)`,id,tid,project,cid,principal,guest,shareID,"guest-photo.jpg",reservation,stamp.Add(time.Duration(ordinal)*time.Second));e!=nil{t.Fatal(e)}
		return id
	}
	first:=insert(collection,"guest",&share.ID,0)
	second:=insert(collection,"guest",&share.ID,1)
	insert(other,"guest",&otherShare.ID,2)
	insert(collection,"user",nil,3)
	page,err:=service.ListGuestUploads(ctx,who,tid,collection,1,"")
	if err!=nil||len(page.Items)!=1||page.Items[0].ID!=first||page.NextCursor==nil{t.Fatalf("first page: %+v %v",page,err)}
	if _,err=admin.Exec(ctx,`UPDATE photo.upload_sessions SET state='verifying',revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,second);err!=nil{t.Fatal(err)}
	next,err:=service.ListGuestUploads(ctx,who,tid,collection,1,*page.NextCursor)
	if err!=nil||len(next.Items)!=1||next.Items[0].ID!=second||next.Items[0].State!="verifying"||next.Items[0].Revision!="2"||next.NextCursor!=nil{t.Fatalf("next page: %+v %v",next,err)}
	if _,err=service.ListGuestUploads(ctx,who,tid,other,1,*page.NextCursor);err==nil{t.Fatal("cursor crossed collection boundary")}
	_,_,_,_,foreignTenant,foreignCollection,_:=sectionAbandonFixture(t)
	if _,err=service.ListGuestUploads(ctx,who,foreignTenant,foreignCollection,10,"");err==nil{t.Fatal("foreign tenant upload metadata admitted")}
	raw,err:=json.Marshal(page);if err!=nil{t.Fatal(err)}
	for _,secret:=range []string{"private-object-key","private-multipart","principal_id","object_key","multipart_id"}{if bytes.Contains(raw,[]byte(secret)){t.Fatalf("private upload metadata leaked: %s",secret)}}
}

func TestIntegrationGuestUploadMembershipAndModerationCAS(t *testing.T){
 ctx,admin,service,who,tid,cid,_:=sectionAbandonFixture(t)
 service.Pepper=[]byte(strings.Repeat("p",32))
 service.Cursors=pagination.Codec{Key:[]byte(strings.Repeat("c",32))}
 share,err:=service.CreateShare(ctx,who,tid,cid,uuid.NewString(),ShareCreate{Label:"Review",Permissions:SharePermissions{Upload:true},Limits:ShareLimits{MaxUploadFiles:10,MaxUploadBytesTotal:1000,MaxFileBytes:100,AllowedFormats:[]string{"jpeg"}},Moderation:"owner_review"})
 if err!=nil{t.Fatal(err)}
 var project,artifact,asset,version,item,snapshot,run uuid.UUID
 if err=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,cid).Scan(&project);err!=nil{t.Fatal(err)}
 digest:=strings.Repeat("a",64)
 if err=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,100,'image/jpeg','working_image','ready',now()) RETURNING id`,tid,uuid.NewString(),digest).Scan(&artifact);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,tid,project,artifact).Scan(&asset);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','pending_review') RETURNING id`,tid,asset,artifact,digest).Scan(&version);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,visibility,moderation_state) VALUES($1,$2,$3,$4,0,'hidden','pending_review') RETURNING id`,tid,cid,asset,version).Scan(&item);err!=nil{t.Fatal(err)}
 guest,uploadID,reservation:=uuid.New(),uuid.New(),uuid.New()
 if err=admin.QueryRow(ctx,`INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','guest',$2,$3,1) RETURNING id`,tid,guest,digest).Scan(&snapshot);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload) VALUES($1,$2,0,$3,$4,'{}')`,tid,snapshot,artifact,digest);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'ingest','completed',$2,'{}',$3,$4,true) RETURNING id`,tid,snapshot,digest,guest).Scan(&run);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,share_id,holder_type,holder_id,bytes,files,expires_at) VALUES($1,$2,'guest',$3,$4,'upload',$5,100,1,now()+interval '1 day')`,reservation,tid,guest,share.ID,uploadID);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,principal_type,principal_id,share_id,expected_size,actual_size,filename,multipart_id,object_key,state,expires_at,reservation_id,staging_candidate_id,ingest_snapshot_id,ingest_run_id,staging_artifact_id,verified_version_id,media_type) VALUES($1,$2,$3,$4,'guest',$5,$6,100,100,'review.jpg','private-multipart','private-object','verified',now()+interval '1 day',$7,$8,$9,$10,$8,$11,'image/jpeg')`,uploadID,tid,project,cid,guest,share.ID,reservation,artifact,snapshot,run,version);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.ingest_upload_bindings(tenant_id,run_id,ordinal,upload_id,source_id,source_sha,project_id,collection_id,principal_type,principal_id,asset_id,version_id,item_id) VALUES($1,$2,0,$3,$4,$5,$6,$7,'guest',$8,$9,$10,$11)`,tid,run,uploadID,artifact,digest,project,cid,guest,asset,version,item);err!=nil{t.Fatal(err)}
 before,err:=service.ListGuestUploads(ctx,who,tid,cid,100,"")
 if err!=nil||len(before.Items)!=1||before.Items[0].ItemID==nil||*before.Items[0].ItemID!=item||before.Items[0].Moderation==nil||*before.Items[0].Moderation!="pending_review"{t.Fatalf("membership projection: %+v %v",before,err)}
 uploads:=upload.Service{DB:service.DB}
 accepted,err:=uploads.Moderate(ctx,who,tid,uploadID,upload.ModerationRequest{Decision:"accept",ExpectedRevision:1})
 if err!=nil||accepted.ID!=item||accepted.AssetVersionID!=version||accepted.Visibility!="hidden"||accepted.Moderation!="accepted"{t.Fatalf("moderation receipt: %+v %v",accepted,err)}
 _,err=uploads.Moderate(ctx,who,tid,uploadID,upload.ModerationRequest{Decision:"reject",ExpectedRevision:1})
 var failure *domain.Error
 if !errors.As(err,&failure)||failure.Code!="REVISION_CONFLICT"{t.Fatalf("stale moderation changed outcome: %v",err)}
 after,err:=service.ListGuestUploads(ctx,who,tid,cid,100,"")
 if err!=nil||len(after.Items)!=1||after.Items[0].Revision!="2"||after.Items[0].Moderation==nil||*after.Items[0].Moderation!="accepted"{t.Fatalf("accepted projection: %+v %v",after,err)}
 // A destination entering deletion must reject even a numerically current
 // upload revision before touching its already accepted catalog outcome.
 if _,err=admin.Exec(ctx,`UPDATE photo.collections SET state='deleting' WHERE tenant_id=$1 AND id=$2`,tid,cid);err!=nil{t.Fatal(err)}
 _,err=uploads.Moderate(ctx,who,tid,uploadID,upload.ModerationRequest{Decision:"reject",ExpectedRevision:2})
 failure=nil
 if !errors.As(err,&failure)||failure.Code!="REVISION_CONFLICT"{t.Fatalf("deleting destination moderation: %v",err)}
 var uploadRevision int64;var outcome string
 if err=admin.QueryRow(ctx,`SELECT u.revision,i.moderation_state FROM photo.upload_sessions u JOIN photo.ingest_upload_bindings b ON b.tenant_id=u.tenant_id AND b.upload_id=u.id AND b.run_id=u.ingest_run_id JOIN photo.collection_items i ON i.tenant_id=b.tenant_id AND i.id=b.item_id WHERE u.tenant_id=$1 AND u.id=$2`,tid,uploadID).Scan(&uploadRevision,&outcome);err!=nil{t.Fatal(err)}
 if uploadRevision!=2||outcome!="accepted"{t.Fatalf("closed destination changed moderation: revision=%d outcome=%s",uploadRevision,outcome)}
}
