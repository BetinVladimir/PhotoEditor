package download

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
 "photoeditor/backend/internal/idempotency"
)

func TestIntentIdempotencyRecordContainsNoBearer(t *testing.T) {
	raw, err := json.Marshal(intentRecord{ID: uuid.New(), ExpiresAt: time.Unix(1, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("token")) {
		t.Fatalf("idempotency record leaks bearer field: %s", raw)
	}
}

func TestIntentRecordLifetimeBoundary(t *testing.T) {
 now := time.Date(2026,10,17,12,0,0,0,time.UTC)
 for _, offset := range []time.Duration{-time.Second,0,66*time.Second} {
  if validIntentRecord(intentRecord{ID:uuid.New(),ExpiresAt:now.Add(offset)},now) { t.Fatal("invalid bearer lifetime accepted",offset) }
 }
 if !validIntentRecord(intentRecord{ID:uuid.New(),ExpiresAt:now.Add(time.Minute)},now) { t.Fatal("live bearer refused") }
 if validIntentRecord(intentRecord{ExpiresAt:now.Add(time.Minute)},now) { t.Fatal("missing intent identity accepted") }
 // An intent accepted at transaction start must fail if it expires before response.
 record := intentRecord{ID:uuid.New(),ExpiresAt:now.Add(time.Second)}
 if !validIntentRecord(record,now) || validIntentRecord(record,now.Add(time.Second)) { t.Fatal("late expiry not enforced") }
}

func TestAcceptedReceiptBindsStatusResourceAndClosedShape(t *testing.T) {
 id:=uuid.New()
 body,_:=json.Marshal(Accepted{DownloadJobID:id,Status:"queued"})
 valid:=idempotency.Response{Status:202,Body:body,ResourceID:&id}
 if got,err:=readAcceptedReceipt(valid);err!=nil||got.DownloadJobID!=id{t.Fatal("valid acceptance refused",err)}
 other:=uuid.New()
 cases:=[]idempotency.Response{
  {Status:200,Body:body,ResourceID:&id},
  {Status:202,Body:body},
  {Status:202,Body:body,ResourceID:&other},
  {Status:202,Body:[]byte(`{"download_job_id":"`+id.String()+`","status":"ready"}`),ResourceID:&id},
  {Status:202,Body:[]byte(`{"download_job_id":"`+id.String()+`","status":"queued","secret":"hidden"}`),ResourceID:&id},
  {Status:202,Body:append(append([]byte{},body...),[]byte(` {}`)...),ResourceID:&id},
 }
 for index,response:=range cases{if _,err:=readAcceptedReceipt(response);err==nil{t.Fatalf("corrupt receipt %d accepted",index)}}
}
