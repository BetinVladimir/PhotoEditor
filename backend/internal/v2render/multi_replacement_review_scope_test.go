package v2render
import (
 "bytes"
 "encoding/json"
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestMultiReplacementReviewScopeStrictIdentity(t *testing.T){
 for _,operation:=range []string{"optical.panorama","optical.focus_stack"}{
  binding,_,_,_:=originalInventoryFixture(t,operation)
  intent,err:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_id":uuid.New(),"params":json.RawMessage(binding.ParamsJCS)})
  if err!=nil{t.Fatal(err)}
  v:=MultiReplacementReviewScope{Tenant:uuid.New(),Project:uuid.New(),Review:uuid.New(),OriginalRun:uuid.New(),OriginalTask:uuid.New(),DecisionHash:strings.Repeat("d",64),Operation:operation,Intent:intent}
  namespace:=uuid.NewSHA1(v.Tenant,[]byte("V29.NegativeReplacement/v2:"+v.Project.String()+":"+v.Review.String()+":"+v.DecisionHash))
  v.Run=uuid.NewSHA1(namespace,[]byte("run:"+v.OriginalRun.String()+":"+v.OriginalTask.String()));v.Workflow,_=v2input.WorkflowID(v.Run);v.Task=uuid.NewSHA1(namespace,[]byte("task:"+v.OriginalTask.String()))
  raw,_:=canonical.Marshal(v)
  got,err:=decodeMultiReplacementReviewScope(raw,v.Tenant,v.Project,v.Review,v.OriginalTask,v.DecisionHash,operation)
  if err!=nil||got.Task!=v.Task||!bytes.Equal(got.Intent,v.Intent){t.Fatal(got,err)}
  for _,alter:=range []func(*MultiReplacementReviewScope){
   func(p *MultiReplacementReviewScope){p.Project=uuid.New()},func(p *MultiReplacementReviewScope){p.Review=uuid.New()},
   func(p *MultiReplacementReviewScope){p.OriginalRun=uuid.New()},func(p *MultiReplacementReviewScope){p.Task=uuid.New()},
   func(p *MultiReplacementReviewScope){p.Workflow=uuid.New()},func(p *MultiReplacementReviewScope){p.Operation="optical.perspective"},
   func(p *MultiReplacementReviewScope){p.Intent=[]byte(`{"schema_version":1}`)},
  }{bad:=v;alter(&bad);body,_:=canonical.Marshal(bad);if _,err:=decodeMultiReplacementReviewScope(body,v.Tenant,v.Project,v.Review,v.OriginalTask,v.DecisionHash,operation);err==nil{t.Fatal("substituted scope accepted")}}
  extra:=append([]byte(`{"unknown":true,`),raw[1:]...)
  if _,err:=decodeMultiReplacementReviewScope(extra,v.Tenant,v.Project,v.Review,v.OriginalTask,v.DecisionHash,operation);err==nil{t.Fatal("unknown field accepted")}
 }
}
func TestMultiReplacementReviewScopeMissingAuthority(t *testing.T){
 s:=MultiProposalService{}
 if _,err:=s.ReplacementReviewScope(context.Background(),auth.Identity{},uuid.New(),uuid.New(),uuid.New(),uuid.New(),strings.Repeat("a",64),"optical.panorama");err==nil{t.Fatal("missing database accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=s.ReplacementReviewScope(ctx,auth.Identity{},uuid.Nil,uuid.Nil,uuid.Nil,uuid.Nil,"","optical.panorama");err!=context.Canceled{t.Fatal(err)}
}
