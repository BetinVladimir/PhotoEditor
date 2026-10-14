package review

import (
 "strings"
 "testing"

 "github.com/google/uuid"
)

func TestRenderResultSelectionBindsDecisionAndDoesNotPromoteRejectedPixels(t *testing.T){
 snapshot:=CandidateSnapshot{TenantID:uuid.New(),ReviewID:uuid.New(),RunID:uuid.New(),Revision:1,State:"pending",Kind:"render.compose",Candidate:ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)},Base:ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)}}
 actor:=uuid.New()
 for _,decision:=range []string{"accept","reject","skip"}{t.Run(decision,func(t *testing.T){
  request:=Decision{ExpectedRevision:1,CandidateHash:snapshot.Candidate.SHA256,Decision:decision}
  plan,err:=PlanCandidate(snapshot,actor,request);if err!=nil{t.Fatal(err)}
  receipt,err:=CompileRenderResultSelection(snapshot,actor,request,plan);if err!=nil{t.Fatal(err)}
  if receipt.SchemaVersion!=1||receipt.ReviewID!=snapshot.ReviewID||receipt.DecisionHash!=plan.DecisionHash||receipt.Route!=plan.Route{t.Fatal("render decision identity lost")}
  switch decision{
  case "accept":if receipt.Image==nil||*receipt.Image!=snapshot.Candidate{t.Fatal("candidate not selected")}
  case "reject":if receipt.Image==nil||*receipt.Image!=snapshot.Base{t.Fatal("rejected candidate promoted")}
  case "skip":if receipt.Image!=nil{t.Fatal("skip promoted image")}
  }
  changed:=plan;changed.Route="candidate"
  if decision!="accept"{if _,err:=CompileRenderResultSelection(snapshot,actor,request,changed);err==nil{t.Fatal("substituted route accepted")}}
  if _,err:=CompileRenderResultSelection(snapshot,uuid.New(),request,plan);err==nil{t.Fatal("foreign actor accepted")}
  negative:=snapshot;negative.Kind="optical.negative"
  if _,err:=CompileRenderResultSelection(negative,actor,request,plan);err==nil{t.Fatal("negative review accepted as render")}
 })}
}
