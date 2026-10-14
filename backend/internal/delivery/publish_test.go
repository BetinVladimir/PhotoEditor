package delivery

import (
 "encoding/json"
 "photoeditor/backend/internal/idempotency"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPublishRejectsInvalidCASBeforeSQL(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, revision := range []string{"", "0", "01", "+1", "-1", "9007199254740991", "9223372036854775808"} {
		tx := &deliveryReadTx{}
		if _, err := PublishPackage(context.Background(), tx, who, uuid.New(), uuid.New(), PublishRequest{ExpectedRevision: revision}, "key"); err == nil || tx.calls != 0 {
			t.Fatal("invalid CAS reached SQL", revision)
		}
	}
	for _, key := range []string{"", " key", "key "} {
		tx := &deliveryReadTx{}
		if _, err := PublishPackage(context.Background(), tx, who, uuid.New(), uuid.New(), PublishRequest{ExpectedRevision: "1"}, key); err == nil || tx.calls != 0 {
			t.Fatal("invalid key reached SQL")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &deliveryReadTx{}
	if _, err := PublishPackage(ctx, tx, who, uuid.New(), uuid.New(), PublishRequest{ExpectedRevision: "1"}, "key"); err == nil || tx.calls != 0 {
		t.Fatal("cancelled publication reached SQL")
	}
}

func TestPublishRequestClosedJSON(t *testing.T){
 var request PublishRequest
 if err:=json.Unmarshal([]byte(`{"expected_revision":"2"}`),&request);err!=nil||request.ExpectedRevision!="2"{t.Fatal(err,request)}
 for _,raw:=range []string{`{"expected_revision":"2","expected_revision":"3"}`,`{"expected_revision":2}`,`{"expected_revision":"2","extra":true}`,`{"expected_revision":null}`,`{}`} {
  if json.Unmarshal([]byte(raw),&request)==nil{t.Fatal("ambiguous publication request accepted",raw)}
 }
}

func TestDeliveryPublicationCachedResponseBinding(t *testing.T){
 tid,id,manifest:=uuid.New(),uuid.New(),uuid.New()
 resource:=map[string]any{"id":id,"revision":"2","document":map[string]any{"artifact_id":manifest,"sha256":rawSHA([]byte("manifest"))},"data":deliveryDataFixture(tid,id,"2","ready",manifest,"2026-10-11T12:00:00Z")}
 raw,err:=json.Marshal(resource);if err!=nil{t.Fatal(err)}
 response:=idempotency.Response{Status:200,ResourceID:&id,Body:raw}
 request:=PublishRequest{ExpectedRevision:"1"}
 if err:=ValidatePublicationResponse(response,tid,id,request);err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"status","missing-resource","foreign-resource","foreign-tenant","revision","extra-root","document-extra"}{t.Run(mode,func(t *testing.T){
  next:=response;nextRequest:=request;nextTenant:=tid
  switch mode{
  case "status":next.Status=202
  case "missing-resource":next.ResourceID=nil
  case "foreign-resource":other:=uuid.New();next.ResourceID=&other
  case "foreign-tenant":nextTenant=uuid.New()
  case "revision":nextRequest.ExpectedRevision="2"
  case "extra-root","document-extra":
   var changed map[string]any;json.Unmarshal(raw,&changed)
   if mode=="extra-root"{changed["bucket"]="secret"}else{changed["document"].(map[string]any)["key"]="secret"}
   next.Body,_=json.Marshal(changed)
  }
  if err:=ValidatePublicationResponse(next,nextTenant,id,nextRequest);err==nil{t.Fatal("malformed cached publication accepted")}
 })}
}

func TestPublishRevisionSafeIncrementBoundaries(t *testing.T){
 for _,value:=range []string{"1","9007199254740990"}{revision,err:=ParsePublishRevision(value);if err!=nil||revision<1||revision+1>9007199254740991{t.Fatal(value,revision,err)}}
}
