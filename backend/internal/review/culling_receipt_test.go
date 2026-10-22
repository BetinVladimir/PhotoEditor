package review

import (
 "encoding/json"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/idempotency"
)

func TestCullingDecisionReceiptRequiresExactStoredTransition(t *testing.T){
 id,run:=uuid.New(),uuid.New();decision,route,hash:="accept","selection",strings.Repeat("b",64)
 v:=View{ID:id,RunID:run,Kind:"culling",State:"decided",Revision:2,CandidateHash:strings.Repeat("a",64),Decision:&decision,DecisionRoute:&route,DecisionHash:&hash,Reasons:json.RawMessage(`[]`),CreatedAt:time.Date(2026,10,17,0,0,0,0,time.UTC)}
 body,e:=canonical.Marshal(v);if e!=nil{t.Fatal(e)};response:=idempotency.Response{Status:200,ResourceID:&id,Body:body};req:=CullingDecision{ExpectedRevision:1,CandidateHash:v.CandidateHash,NeverDelete:true}
 if _,e=readCullingDecisionReceipt(response,id,run,req);e!=nil{t.Fatal(e)}
 wrong:=uuid.New();badStatus:=response;badStatus.Status=202;badResource:=response;badResource.ResourceID=&wrong
 var fields map[string]any;if e=json.Unmarshal(body,&fields);e!=nil{t.Fatal(e)};fields["unexpected"]=true;extra,e:=json.Marshal(fields);if e!=nil{t.Fatal(e)};badShape:=response;badShape.Body=extra
 for _,bad:=range []idempotency.Response{badStatus,badResource,badShape}{if _,e=readCullingDecisionReceipt(bad,id,run,req);e==nil{t.Fatal("unbound stored receipt accepted")}}
 for _,mutate:=range []func(*View){func(v *View){v.RunID=wrong},func(v *View){v.Revision=1},func(v *View){v.DecisionHash=nil},func(v *View){v.CreatedAt=time.Time{}}}{bad:=v;mutate(&bad);response.Body,e=canonical.Marshal(bad);if e!=nil{t.Fatal(e)};if _,e=readCullingDecisionReceipt(response,id,run,req);e==nil{t.Fatal("corrupt transition accepted")}}
}
