package upload

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/idempotency"
)

func TestCompletionReceiptBindsStatusResourceAndClosedBody(t *testing.T){
 id:=uuid.New();valid:=idempotency.Response{Status:202,ResourceID:&id,Body:json.RawMessage(`{"job_id":"`+id.String()+`","status":"queued"}`)}
 if _,err:=readCompletionReceipt(valid,id);err!=nil{t.Fatal(err)}
 for _,raw:=range []string{`{"job_id":"`+uuid.NewString()+`","status":"queued"}`,`{"job_id":"`+id.String()+`","status":"completed"}`,`{"job_id":"`+id.String()+`","status":"queued","extra":true}`} {
  changed:=valid;changed.Body=json.RawMessage(raw);if _,err:=readCompletionReceipt(changed,id);err==nil{t.Fatal("accepted corrupt receipt",raw)}
 }
 for _,status:=range []int{200,201}{changed:=valid;changed.Status=status;if _,err:=readCompletionReceipt(changed,id);err==nil{t.Fatal("wrong receipt status",status)}}
}
