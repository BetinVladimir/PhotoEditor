package delivery

import (
 "context"

 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

func(s DelegatedCandidateReviewReader)ReadDelegatedRenderReview(ctx context.Context,
 task provenance.CanonicalSigningTask,root,report v2input.Ref,
 required bool)(*provenance.SigningRenderReview,error){
 if task.Operation!="render.compose"||task.Version!="1"{
  return nil,v2input.ErrInvalid
 }
 approval,err:=s.ReadDelegatedCandidateReview(ctx,task,root,report,required)
 if err!=nil||approval==nil{return nil,err}
 return &provenance.SigningRenderReview{ID:approval.ID,Task:approval.Task,
  Result:approval.Result,Root:approval.Root,Report:approval.Report,
  Accepted:approval.Accepted},nil
}

func(s DelegatedCandidateReviewReader)ReadDelegatedNegativeReview(ctx context.Context,
 task provenance.CanonicalSigningTask,root,report v2input.Ref)(*provenance.SigningNegativeReview,error){
 if task.Operation!="optical.negative"||task.Version!="1"||
  !task.ReviewRequired{return nil,v2input.ErrInvalid}
 approval,err:=s.ReadDelegatedCandidateReview(ctx,task,root,report,true)
 if err!=nil{return nil,err}
 if approval==nil{return nil,v2input.ErrInvalid}
 return &provenance.SigningNegativeReview{ID:approval.ID,Task:approval.Task,
  Result:approval.Result,Root:approval.Root,Report:approval.Report,
  Accepted:approval.Accepted},nil
}

var _ provenance.DelegatedRenderReviewPort = DelegatedCandidateReviewReader{}
var _ provenance.DelegatedNegativeReviewPort = DelegatedCandidateReviewReader{}
