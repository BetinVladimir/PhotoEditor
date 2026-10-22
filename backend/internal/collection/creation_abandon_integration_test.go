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

// Actual API-role transactions. Authored only; not executed in coding phase.
func TestIntegrationCreationAbandonWinsBeforeLateCreate(t *testing.T){
 ctx,admin,service,who,tid,parent,_:=sectionAbandonFixture(t)
 service.Pepper=[]byte(strings.Repeat("p",32))
 share:=shareIntentRequest();past:=time.Now().UTC().Add(-time.Hour);share.ExpiresAt=OptionalTime{Set:true,Value:&past}
 key:=uuid.NewString();out,err:=service.AbandonShareCreation(ctx,who,tid,parent,key,share)
 if err!=nil||!isCreationAbandoned(out,"Share.Create",key,parent){t.Fatalf("abandon expired intent: %+v %v",out,err)}
 _,err=service.CreateShare(ctx,who,tid,parent,key,share);var failure *domain.Error
 if !errors.As(err,&failure)||failure.Code!="SHARE_CREATION_ABANDONED"{t.Fatalf("late create escaped: %v",err)}
 changed:=share;changed.Label="different";_,err=service.AbandonShareCreation(ctx,who,tid,parent,key,changed)
 if !errors.As(err,&failure)||failure.Code!="IDEMPOTENCY_CONFLICT"{t.Fatalf("changed intent: %v",err)}
 var project uuid.UUID;if err=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,parent).Scan(&project);err!=nil{t.Fatal(err)}
 req:=Create{ProjectID:project,Name:"Abandoned empty",InitialShare:&share};collectionKey:=uuid.NewString()
 out,err=service.AbandonCreation(ctx,who,tid,collectionKey,req);if err!=nil||!isCreationAbandoned(out,"Collection.Create",collectionKey,project){t.Fatalf("collection abandon: %+v %v",out,err)}
 _,err=service.Create(ctx,who,tid,collectionKey,req);if !errors.As(err,&failure)||failure.Code!="COLLECTION_CREATION_ABANDONED"{t.Fatalf("late collection: %v",err)}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.share_links WHERE tenant_id=$1 AND collection_id=$2`,tid,parent).Scan(&count);err!=nil||count!=0{t.Fatalf("loser side effects: %d %v",count,err)}
 if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.collections WHERE tenant_id=$1 AND project_id=$2 AND name=$3`,tid,project,req.Name).Scan(&count);err!=nil||count!=0{t.Fatalf("loser collection: %d %v",count,err)}
}
func TestIntegrationCreationAcceptedWinsAbandonNeverReturnsSecret(t *testing.T){
 ctx,admin,service,who,tid,parent,_:=sectionAbandonFixture(t);service.Pepper=[]byte(strings.Repeat("p",32));req:=shareIntentRequest();key:=uuid.NewString()
 first,err:=service.CreateShare(ctx,who,tid,parent,key,req);if err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.collections SET state='deleting' WHERE tenant_id=$1 AND id=$2`,tid,parent);err!=nil{t.Fatal(err)}
 service.Pepper=nil;out,err:=service.AbandonShareCreation(ctx,who,tid,parent,key,req);var received ShareCreated
 if err!=nil||out.Status!=201||json.Unmarshal(out.Body,&received)!=nil||received.ID!=first.ID||received.Secret!=""{t.Fatalf("accepted receipt: %+v %v",out,err)}
 var project uuid.UUID;if err=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,parent).Scan(&project);err!=nil{t.Fatal(err)}
 collectionReq:=Create{ProjectID:project,Name:"Accepted empty"};collectionKey:=uuid.NewString();created,err:=service.Create(ctx,who,tid,collectionKey,collectionReq);if err!=nil{t.Fatal(err)}
 got,err:=service.AbandonCreation(ctx,who,tid,collectionKey,collectionReq);if err!=nil||got.Status!=201||got.ResourceID==nil||created.ResourceID==nil||*got.ResourceID!=*created.ResourceID{t.Fatalf("accepted collection: %+v %v",got,err)}
}
func TestIntegrationCreationAbandonPromotesExistingUnsealedExpiry(t *testing.T){
 ctx,admin,service,who,tid,parent,_:=sectionAbandonFixture(t);req:=shareIntentRequest();intent,err:=freezeShareIntent(parent,req);if err!=nil{t.Fatal(err)};hash,_:=canonical.Hash(intent);key:=uuid.NewString()
 if _,err=admin.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,'Share.Create',$3,$4,now()+interval '1 second')`,tid,who.UserID,key,hash);err!=nil{t.Fatal(err)}
 out,err:=service.AbandonShareCreation(ctx,who,tid,parent,key,req);if err!=nil||out.Status!=200{t.Fatalf("seal: %+v %v",out,err)}
 var permanent bool;if err=admin.QueryRow(ctx,`SELECT expires_at='9999-01-01T00:00:00Z'::timestamptz FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Share.Create' AND key=$3`,tid,who.UserID,key).Scan(&permanent);err!=nil||!permanent{t.Fatalf("expiry not promoted: %v",err)}
}
func TestIntegrationShareAbandonPreservesProvenHistoricalIntentAndCurrentSession(t *testing.T){
 ctx,admin,service,who,tid,parent,_:=sectionAbandonFixture(t);service.Pepper=[]byte(strings.Repeat("p",32));req:=shareIntentRequest();expiry:=time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond);req.ExpiresAt=OptionalTime{Set:true,Value:&expiry}
 key:=uuid.NewString();first,err:=service.CreateShare(ctx,who,tid,parent,key,req);if err!=nil{t.Fatal(err)}
 // Convert only the fingerprint to the actual historical request form while
 // retaining the genuine accepted immutable response and existing resource.
 legacy:=legacyShareCreateRequest{parent,req.Label,&expiry,req.Permissions,req.Limits,req.Moderation,req.AllowedPresets};hash,_:=canonical.Hash(legacy)
 if _,err=admin.Exec(ctx,`UPDATE photo.idempotency_records SET request_hash=$4 WHERE tenant_id=$1 AND principal_id=$2 AND route='Share.Create' AND key=$3`,tid,who.UserID,key,hash);err!=nil{t.Fatal(err)}
 out,err:=service.AbandonShareCreation(ctx,who,tid,parent,key,req);if err!=nil||out.Status!=201||out.ResourceID==nil||*out.ResourceID!=first.ID{t.Fatalf("historical explicit receipt: %+v %v",out,err)}
 omitted:=req;omitted.ExpiresAt=OptionalTime{};_,err=service.AbandonShareCreation(ctx,who,tid,parent,key,omitted);var failure *domain.Error;if !errors.As(err,&failure)||failure.Code!="SHARE_REPLAY_INTENT_REQUIRED"{t.Fatalf("guessed historical omitted expiry: %v",err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.sessions SET revoked_at=now() WHERE id=$1`,who.SessionID);err!=nil{t.Fatal(err)}
 if _,err=service.AbandonShareCreation(ctx,who,tid,parent,key,req);err==nil{t.Fatal("revoked current session recovered receipt")}
 var stored string;if err=admin.QueryRow(ctx,`SELECT request_hash FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Share.Create' AND key=$3`,tid,who.UserID,key).Scan(&stored);err!=nil||stored!=hash{t.Fatalf("historical identity rewritten: %v",err)}
}
