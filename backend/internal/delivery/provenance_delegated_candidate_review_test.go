package delivery

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedCandidateReviewRequiresBoundEvidence(t *testing.T){
 reader:=DelegatedCandidateReviewReader{}
 task:=provenance.CanonicalSigningTask{Task:uuid.New(),
  Operation:"render.compose",Version:"1"}
 candidate:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 if _,err:=reader.lookup(context.Background(),task,candidate);err==nil{
  t.Fatal("missing delegated review SQL scope accepted")
 }
 if _,err:=reader.ReadDelegatedCandidateReview(context.Background(),
  task,candidate,candidate,false);err==nil{
  t.Fatal("missing review documents accepted")
 }
 if _,err:=reader.ReadDelegatedNegativeReview(context.Background(),task,
  candidate,candidate);err==nil{t.Fatal("render task accepted as negative review")}
 if _,err:=reader.ReadDelegatedRenderReview(context.Background(),
  provenance.CanonicalSigningTask{Operation:"optical.negative",Version:"1"},
  candidate,candidate,true);err==nil{t.Fatal("negative task accepted as render review")}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=reader.ReadDelegatedCandidateReview(ctx,task,candidate,
  candidate,false);!errors.Is(err,context.Canceled){
  t.Fatal("review cancellation lost",err)
 }
}
