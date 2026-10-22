package v2workflow
import("encoding/json";"testing";"github.com/google/uuid";"photoeditor/backend/internal/idempotency")
func TestCancelReceiptBindsImmutableWorkflowStatus(t *testing.T){
 workflow,run:=uuid.New(),uuid.New()
 body:=Status{WorkflowID:workflow,RunID:run,Revision:"2",State:"cancelling",Outputs:[]Ref{}}
 raw,_:=json.Marshal(body);out:=idempotency.Response{Status:200,ResourceID:&workflow,Body:raw}
 if err:=validateCancelReceipt(out,workflow);err!=nil{t.Fatal(err)}
 for _,change:=range []func(*Status){func(v *Status){v.WorkflowID=uuid.New()},func(v *Status){v.RunID=uuid.Nil},func(v *Status){v.Revision="9223372036854775808"},func(v *Status){v.State="running"},func(v *Status){v.Outputs=nil},func(v *Status){e:="internal";v.ErrorCode=&e}}{v:=body;change(&v);out.Body,_=json.Marshal(v);if validateCancelReceipt(out,workflow)==nil{t.Fatal("malformed accepted status escaped",string(out.Body))}}
 out.Body=append(raw[:len(raw)-1],[]byte(`,"extra":true}`)...);if validateCancelReceipt(out,workflow)==nil{t.Fatal("extra receipt field escaped")}
}
