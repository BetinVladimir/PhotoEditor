package semanticproposal

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestScenePolicyRequiresExactQualifiedPassingEvidence(t *testing.T){
 policy:=v2input.SemanticScenePolicy{Algorithm:"farneback_depth_plane_v1",BackgroundLabels:[]int{0,1},ProxyEdge:512,MinSupport:64,FlowErrorPX:.5,HomographyErrorPX:.5,DepthPlaneError:.01}
 raw,err:=canonical.Marshal(policy);if err!=nil{t.Fatal(err)}
 _,sha,err:=v2input.DecodeSemanticScenePolicy(raw);if err!=nil{t.Fatal(err)}
 expected:=v2qualification.Expected{EvidenceSHAs:map[string]struct{}{sha:{}},RequiredChecks:[]string{semanticSceneCheck}}
 report:=v2qualification.Report{Checks:[]v2qualification.Check{{ID:semanticSceneCheck,Passed:true,EvidenceSHA:sha}}}
 body,got,err:=scenePolicyFromVerifiedReport(raw,expected,report);if err!=nil||got!=sha||body!=string(raw){t.Fatalf("exact policy: %v",err)}
 for _,checks:=range [][]v2qualification.Check{nil,{{ID:semanticSceneCheck,Passed:false,EvidenceSHA:sha}},{{ID:semanticSceneCheck,Passed:true,EvidenceSHA:"wrong"}},{report.Checks[0],report.Checks[0]}}{
  if _,_,err=scenePolicyFromVerifiedReport(raw,expected,v2qualification.Report{Checks:checks});err==nil{t.Fatal("unqualified or duplicate evidence accepted")}
 }
 for _,missing:=range []v2qualification.Expected{{EvidenceSHAs:expected.EvidenceSHAs},{RequiredChecks:expected.RequiredChecks}}{
  if _,_,err=scenePolicyFromVerifiedReport(raw,missing,report);err==nil{t.Fatal("missing trusted policy binding accepted")}
 }
}
func TestScenePolicyCancelledBeforeCatalogIO(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 proof,err:=(SQLTransferSourceReader{}).readSemanticScenePolicy(ctx,auth.Identity{},uuid.Nil,v2input.SemanticQualificationPin{},v2qualification.Expected{},nil)
 if !errors.Is(err,context.Canceled)||proof!=(semanticScenePolicyProof{}){t.Fatal("cancelled policy retained proof")}
}
