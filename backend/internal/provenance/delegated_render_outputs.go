package provenance

import "context"

func ResolveDelegatedRenderOutputs(ctx context.Context,
 source DelegatedRenderEvidenceReader, reviews DelegatedRenderReviewPort,
 task CanonicalSigningTask) (ResolvedSigningRenderOutputs,error) {
 var zero ResolvedSigningRenderOutputs
 if err:=ctx.Err();err!=nil{return zero,err}
 if source==nil||reviews==nil{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 isolated,err:=ownSigningProducerTask(owned)
 if err!=nil{return zero,err}
 evidence,err:=source.ReadDelegatedRenderEvidence(ctx,isolated)
 if err!=nil{return zero,err}
 evidenceDigest,err:=signingRenderEvidenceDigest(evidence)
 if err!=nil{return zero,err}
 required:=owned.ReviewRequired||evidence.inheritedReview||evidence.report.Status=="review"
 readReview:=func()(*SigningRenderReview,error){
  isolated,err:=ownSigningProducerTask(owned)
  if err!=nil{return nil,err}
  return reviews.ReadDelegatedRenderReview(ctx,isolated,evidence.pixels.root,
   evidence.pixels.result.Report,required)
 }
 approval,err:=readReview()
 if err!=nil{return zero,err}
 var frozen *SigningRenderReview
 if approval!=nil{value:=*approval;frozen=&value}
 action,err:=CompileSigningRenderAction(owned,evidence,frozen)
 if err!=nil{return zero,err}
 children,err:=CompileSigningRenderChildOutputs(owned,evidence)
 if err!=nil{return zero,err}
 current,err:=readReview()
 if err!=nil{return zero,err}
 currentDigest,err:=signingRenderEvidenceDigest(evidence)
 if err!=nil||currentDigest!=evidenceDigest{return zero,ErrInvalid}
 if (current==nil)!=(frozen==nil)||
  current!=nil&&*current!=*frozen{return zero,ErrInvalid}
 return ResolvedSigningRenderOutputs{Action:action,Children:children},ctx.Err()
}
