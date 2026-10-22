package v2render
import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestMultiRunReviewStrictFrozenScope(t *testing.T){
 for _,op:=range []string{"optical.panorama","optical.focus_stack"}{
  binding,_,_,_:=originalInventoryFixture(t,op)
  intent,_:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_id":uuid.New(),"params":json.RawMessage(binding.ParamsJCS)})
  v:=MultiRunReviewScope{Tenant:uuid.New(),Project:uuid.New(),ReferenceAsset:uuid.New(),Run:uuid.New(),Operation:op,Intent:intent};v.Workflow,_=v2input.WorkflowID(v.Run);v.Task=uuid.NewSHA1(v.Workflow,[]byte(op+":0"))
  raw,_:=canonical.Marshal(v);if _,err:=decodeMultiRunReviewScope(raw,v.Tenant,v.Run);err!=nil{t.Fatal(err)}
  for _,alter:=range []func(*MultiRunReviewScope){func(p *MultiRunReviewScope){p.Tenant=uuid.New()},func(p *MultiRunReviewScope){p.Run=uuid.New()},func(p *MultiRunReviewScope){p.Project=uuid.Nil},func(p *MultiRunReviewScope){p.ReferenceAsset=uuid.Nil},func(p *MultiRunReviewScope){p.Task=uuid.New()},func(p *MultiRunReviewScope){p.Workflow=uuid.New()},func(p *MultiRunReviewScope){p.Operation="optical.negative"},func(p *MultiRunReviewScope){p.Intent=[]byte(`{}`)}}{
   bad:=v;alter(&bad);body,_:=canonical.Marshal(bad);if _,err:=decodeMultiRunReviewScope(body,v.Tenant,v.Run);err==nil{t.Fatal("substituted scope accepted")}
  }
  extra:=append([]byte(`{"unknown":true,`),raw[1:]...);if _,err:=decodeMultiRunReviewScope(extra,v.Tenant,v.Run);err==nil{t.Fatal("unknown field accepted")}
 }
}
func TestMultiRunReviewCancellationAndMissingAuthority(t *testing.T){
 s:=MultiProposalService{};ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=s.RunReviewScope(ctx,auth.Identity{},uuid.Nil,uuid.Nil);err!=context.Canceled{t.Fatal(err)}
 if _,err:=s.RunReviewScope(context.Background(),auth.Identity{},uuid.New(),uuid.New());err==nil{t.Fatal("missing authority accepted")}
}
