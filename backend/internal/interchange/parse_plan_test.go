package interchange

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
func TestParsePlanPinsParamsBudgetsAndOwnsReturnedBytes(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant:=uuid.New()
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 request:=CreateRequest{ProjectID:uuid.New(),Direction:"import",Format:"photoeditor_bundle",Bundle:&ref}
 limits:=ParseLimits{RAMBytes:2<<30,ScratchBytes:1<<30,OutputBytes:1<<30,ExpandedBytes:1024,Files:10}
 plan,err:=CompileParsePlan(tenant,who,request,"key",BundleEvidence{ref,100},limits);if err!=nil{t.Fatal(err)}
 params,paramsRef,raw:=plan.Params();if params.Request.Bundle!=ref||paramsRef.ArtifactID!=plan.Identity().Params||plan.OutputBytes()!=1024+(8<<20){t.Fatal("wrong frozen plan")}
 raw[0]='x';_,_,again:=plan.Params();if again[0]!='{'{t.Fatal("Params bytes aliased")}
 inputs:=plan.DirectInputs();inputs[0].Contract="corrupted";if plan.DirectInputs()[0].Contract!="Params"{t.Fatal("inputs aliased")}
 draft,err:=plan.DraftTask(ref.SHA256);if err!=nil||draft.ID!=plan.Identity().Task||draft.Operation!="v2.interchange.parse"||draft.ResourceClass!="cpu"{t.Fatal("draft mismatch",err)}

 var wire map[string]any;if json.Unmarshal(draft.ExecutionProfile,&wire)!=nil{t.Fatal("profile invalid")}
 estimate:=wire["resource_estimate"].(map[string]any)
 if estimate["vram_bytes"]!=float64(0)||estimate["scratch_bytes"]!=float64(100+1024+(8<<20)){t.Fatal("wrong resource estimate")}
 draft.ExecutionProfile[0]='x';next,_:=plan.DraftTask(ref.SHA256);if next.ExecutionProfile[0]!='{'{t.Fatal("profile aliased")}
 for _,change:=range []func(*ParseLimits){func(l *ParseLimits){l.RAMBytes=1},func(l *ParseLimits){l.ScratchBytes=1},func(l *ParseLimits){l.OutputBytes=1},func(l *ParseLimits){l.Files=30001}}{
  invalid:=limits;change(&invalid);if _,err:=CompileParsePlan(tenant,who,request,"key",BundleEvidence{ref,100},invalid);err==nil{t.Fatal("invalid budget accepted")}
 }
 foreign:=ref;foreign.ArtifactID=uuid.New();if _,err:=CompileParsePlan(tenant,who,request,"key",BundleEvidence{foreign,100},limits);err==nil{t.Fatal("foreign bundle accepted")}
 if _,err:=(ParsePlan{}).DraftTask(ref.SHA256);err==nil{t.Fatal("empty plan accepted")}
}
