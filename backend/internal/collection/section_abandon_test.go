package collection

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/idempotency"
)
func TestSectionAbandonRequiresExactTombstoneIdentity(t *testing.T){
 collection,key:=uuid.New(),uuid.New().String();body,err:=sectionAbandonBody(collection,key);if err!=nil{t.Fatal(err)}
 valid:=idempotency.Response{Status:200,Body:body};if !isSectionAbandoned(valid,collection,key){t.Fatal("valid tombstone refused")}
 for _,v:=range []idempotency.Response{{Status:201,Body:body},{Status:200,Body:body,ResourceID:&collection},{Status:200,Body:json.RawMessage(`{"status":"abandoned"}`)}}{if isSectionAbandoned(v,collection,key){t.Fatal("invalid tombstone accepted")}}
 if isSectionAbandoned(valid,uuid.New(),key)||isSectionAbandoned(valid,collection,uuid.New().String()){t.Fatal("foreign identity accepted")}
}
