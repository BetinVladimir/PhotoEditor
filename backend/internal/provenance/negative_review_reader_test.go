package provenance

import (
 "context"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestNegativeCandidateDecisionBindsKindAndExactSnapshot(t *testing.T){
 for _,mode:=range []string{"valid","kind","actor","base","candidate","selection","hash"}{
  t.Run(mode,func(t *testing.T){
   sha:=strings.Repeat("a",64);actor:=uuid.New()
   snapshot:=review.CandidateSnapshot{TenantID:uuid.New(),ReviewID:uuid.New(),RunID:uuid.New(),Revision:1,State:"pending",Kind:"optical.negative",Candidate:review.ArtifactRef{ID:uuid.New(),SHA256:sha},Base:review.ArtifactRef{ID:uuid.New(),SHA256:sha}}
   plan,err:=review.PlanCandidate(snapshot,actor,review.Decision{ExpectedRevision:1,CandidateHash:sha,Decision:"accept"});if err!=nil{t.Fatal(err)}
   raw,err:=canonical.Marshal(plan.Selection);if err!=nil{t.Fatal(err)}
   hash:=plan.DecisionHash
   switch mode{
   case "kind":snapshot.Kind="render.compose"
   case "actor":actor=uuid.New()
   case "base":snapshot.Base.ID=uuid.New()
   case "candidate":snapshot.Candidate.ID=uuid.New()
   case "selection":raw=[]byte(`{}`)
   case "hash":hash=strings.Repeat("b",64)
   }
   _,err=verifySigningCandidateDecision(snapshot,actor,raw,hash)
   if mode=="valid"{if err!=nil{t.Fatal(err)};return}
   if err==nil{t.Fatal("changed negative approval accepted",mode)}
  })
 }
}

func TestNegativeReviewReaderRequiresDependenciesAndCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(SQLSigningNegativeReviewReader{}).ReadSigningNegativeReview(ctx,auth.Identity{},ReadySigningExport{},CanonicalSigningTask{},v2input.Ref{},v2input.Ref{});err!=context.Canceled{t.Fatal(err)}
 if _,err:=(SQLSigningNegativeReviewReader{}).ReadSigningNegativeReview(context.Background(),auth.Identity{},ReadySigningExport{},CanonicalSigningTask{},v2input.Ref{},v2input.Ref{});err==nil{t.Fatal("missing dependencies accepted")}
}
