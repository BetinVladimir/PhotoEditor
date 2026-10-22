package collection

import (
 "encoding/json"
 "errors"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
)

func shareIntentRequest() ShareCreate {
 return ShareCreate{Label:"Client",Permissions:SharePermissions{View:true},Limits:ShareLimits{MaxUploadFiles:1,MaxUploadBytesTotal:1024,MaxFileBytes:1024,AllowedFormats:[]string{"jpg"}},Moderation:"owner_review"}
}

// Authored against the actual API-role service and durable SQL receipt, not a
// mock clock or an approximation of idempotency.Use. Not executed in coding phase.
func TestIntegrationShareOriginalExpiryIntentReplay(t *testing.T) {
 ctx,admin,service,who,tid,collection,_:=sectionAbandonFixture(t)
 service.Pepper=[]byte(strings.Repeat("p",32));req:=shareIntentRequest();key:=uuid.NewString()
 first,err:=service.CreateShare(ctx,who,tid,collection,key,req);if err!=nil{t.Fatal(err)}
 // A mutable current policy and absent current secret dependency must not alter
 // the immutable accepted result. The replay never returns another secret.
 if _,err=admin.Exec(ctx,`UPDATE photo.share_links SET label='Changed',policy_version=2 WHERE tenant_id=$1 AND id=$2`,tid,first.ID);err!=nil{t.Fatal(err)}
 service.Pepper=nil
 second,err:=service.CreateShare(ctx,who,tid,collection,key,req)
 if err!=nil||second.ID!=first.ID||!second.Replayed||second.Secret!=""||second.Label!="Client"||!second.ExpiresAt.Equal(*first.ExpiresAt){t.Fatalf("omitted replay: %+v %v",second,err)}
 changed:=req;changed.ExpiresAt=OptionalTime{Set:true}
 _,err=service.CreateShare(ctx,who,tid,collection,key,changed);var failure *domain.Error
 if !errors.As(err,&failure)||failure.Code!="IDEMPOTENCY_CONFLICT"{t.Fatalf("null changed omitted intent: %v",err)}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.share_links WHERE tenant_id=$1 AND collection_id=$2`,tid,collection).Scan(&count);err!=nil||count!=1{t.Fatalf("duplicate shares: %d %v",count,err)}
}

func TestIntegrationShareExpiredAcceptedAndHistoricalIntentBoundary(t *testing.T) {
 ctx,admin,service,who,tid,collection,_:=sectionAbandonFixture(t)
 service.Pepper=[]byte(strings.Repeat("p",32));req:=shareIntentRequest();expiry:=time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond);req.ExpiresAt=OptionalTime{Set:true,Value:&expiry}
 // Seed a genuine immutable accepted receipt from a past successful operation;
 // current temporal admission must never reject its exact replay.
 id:=uuid.New();permissions,_:=canonical.Marshal(req.Permissions);limits,_:=canonical.Marshal(req.Limits);presets,_:=canonical.Marshal(req.AllowedPresets)
 frozen:=ShareCreated{Share:Share{ID:id,CollectionID:collection,Label:req.Label,Permissions:permissions,PolicyVersion:"1",ExpiresAt:&expiry,Limits:limits,Moderation:req.Moderation,AllowedPresets:presets,CreatedAt:expiry.Add(-time.Hour)}}
 body,_:=canonical.Marshal(frozen);intent,err:=freezeShareIntent(collection,req);if err!=nil{t.Fatal(err)};hash,_:=canonical.Hash(intent);key:=uuid.NewString()
 insert:=func(key,hash string){t.Helper();if _,e:=admin.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,response_status,response_body,resource_id,expires_at) VALUES($1,'user',$2,'Share.Create',$3,$4,201,$5,$6,now()+interval '1 day')`,tid,who.UserID,key,hash,json.RawMessage(body),id);e!=nil{t.Fatal(e)}}
 insert(key,hash)
 replay,err:=service.CreateShare(ctx,who,tid,collection,key,req);if err!=nil||replay.ID!=id||!replay.Replayed||replay.Secret!=""{t.Fatalf("expired accepted: %+v %v",replay,err)}
 legacy:=legacyShareCreateRequest{collection,req.Label,&expiry,req.Permissions,req.Limits,req.Moderation,req.AllowedPresets};oldHash,_:=canonical.Hash(legacy);oldKey:=uuid.NewString();insert(oldKey,oldHash)
 replay,err=service.CreateShare(ctx,who,tid,collection,oldKey,req);if err!=nil||replay.ID!=id||!replay.Replayed{t.Fatalf("exact frozen legacy expiry: %+v %v",replay,err)}
 omitted:=req;omitted.ExpiresAt=OptionalTime{}
 _,err=service.CreateShare(ctx,who,tid,collection,oldKey,omitted);var failure *domain.Error
 if !errors.As(err,&failure)||failure.Code!="SHARE_REPLAY_INTENT_REQUIRED"{t.Fatalf("unprovable old intent: %v",err)}
 var after string;if err=admin.QueryRow(ctx,`SELECT request_hash FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Share.Create' AND key=$3`,tid,who.UserID,oldKey).Scan(&after);err!=nil||after!=oldHash{t.Fatalf("historical hash rewritten: %v",err)}
}

func TestIntegrationCollectionInitialShareReplayWithoutFreshSecretDependency(t *testing.T){
 ctx,admin,service,who,tid,collection,_:=sectionAbandonFixture(t);service.Pepper=[]byte(strings.Repeat("p",32));var project uuid.UUID
 if err:=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,collection).Scan(&project);err!=nil{t.Fatal(err)}
 share:=shareIntentRequest();share.Permissions=SharePermissions{Download:true};req:=Create{ProjectID:project,Name:"Receive and deliver",InitialShare:&share};key:=uuid.NewString()
 first,err:=service.Create(ctx,who,tid,key,req);if err!=nil{t.Fatal(err)};var created CollectionCreated;if err=json.Unmarshal(first.Body,&created);err!=nil||created.InitialShare==nil||created.InitialShare.Secret==""||created.Collection.State!="published"{t.Fatalf("atomic create: %+v %v",created,err)}
 service.Pepper=nil;again,err:=service.Create(ctx,who,tid,key,req);if err!=nil||!again.Replayed||again.ResourceID==nil||*again.ResourceID!=created.Collection.ID{t.Fatalf("initial share replay: %+v %v",again,err)}
 var replay CollectionCreated;if err=json.Unmarshal(again.Body,&replay);err!=nil||replay.InitialShare==nil||replay.InitialShare.Secret!=""||replay.InitialShare.ID!=created.InitialShare.ID{t.Fatalf("secret exposed or share changed: %+v %v",replay,err)}
}
