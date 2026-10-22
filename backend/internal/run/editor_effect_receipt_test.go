package run

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestEditorEffectReceiptExactAcceptedContract(t *testing.T) {
 id:=uuid.New()
 body,err:=canonical.Marshal(ApplyAccepted{id,uuid.Nil,"queued"});if err!=nil{t.Fatal(err)}
 if !validEditorEffectReceipt(202,body,id){t.Fatal("accepted original ticket refused")}
 for name,check:=range map[string]struct{status int;body []byte;id uuid.UUID}{
  "wrong status":{201,body,id},"foreign run":{202,body,uuid.New()},
  "missing fields":{202,[]byte(`{"run_id":"`+id.String()+`","status":"queued"}`),id},
  "foreign collection":{202,mustEditorTicket(t,ApplyAccepted{id,uuid.New(),"queued"}),id},
  "terminal invented ticket":{202,mustEditorTicket(t,ApplyAccepted{id,uuid.Nil,"completed"}),id},
  "unknown field":{202,[]byte(`{"run_id":"`+id.String()+`","output_collection_id":"00000000-0000-0000-0000-000000000000","status":"queued","workflow_id":"`+id.String()+`"}`),id},
 } {t.Run(name,func(t *testing.T){if validEditorEffectReceipt(check.status,check.body,check.id){t.Fatal("invalid receipt accepted")}})}
}
func mustEditorTicket(t *testing.T,v ApplyAccepted)[]byte{t.Helper();raw,e:=canonical.Marshal(v);if e!=nil{t.Fatal(e)};return raw}
