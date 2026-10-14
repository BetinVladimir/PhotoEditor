package review

import (
 "strings"
 "testing"

 "github.com/google/uuid"
)

func TestNegativeResultSelectionFollowsExactHumanDecision(t *testing.T){
 snapshot:=CandidateSnapshot{TenantID:uuid.New(),ReviewID:uuid.New(),RunID:uuid.New(),Revision:1,State:"pending",Kind:"optical.negative",Candidate:ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)},Base:ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)}}
 actor:=uuid.New()
 for _,decision:=range []string{"accept","reject","skip"}{
  t.Run(decision,func(t *testing.T){
   request:=Decision{ExpectedRevision:1,CandidateHash:snapshot.Candidate.SHA256,Decision:decision}
   persisted,err:=PlanCandidate(snapshot,actor,request);if err!=nil{t.Fatal(err)}
   result,err:=CompileNegativeResultSelection(snapshot,actor,request,persisted);if err!=nil{t.Fatal(err)}
   if result.ReviewID!=snapshot.ReviewID||result.DecisionHash!=persisted.DecisionHash{t.Fatal("decision identity lost")}
   switch decision{case "accept":if result.Image==nil||*result.Image!=snapshot.Candidate{t.Fatal("candidate not selected")};case "reject":if result.Image==nil||*result.Image!=snapshot.Base{t.Fatal("rejected candidate promoted")};case "skip":if result.Image!=nil{t.Fatal("skip promoted an image")}}
   corrupt:=persisted;corrupt.Route="other"
   if _,err:=CompileNegativeResultSelection(snapshot,actor,request,corrupt);err==nil{t.Fatal("tampered selection accepted")}
   if _,err:=CompileNegativeResultSelection(snapshot,uuid.New(),request,persisted);err==nil{t.Fatal("foreign actor accepted")}
  })
 }
}
