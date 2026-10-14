package provenance

import (
 "context"

 "photoeditor/backend/internal/canonical"
)

type BoundDelegatedRenderReviewedCausalSource struct {
 Causal BoundDelegatedRenderCausalSource
 Reviews DelegatedRenderReviewPort
}

type delegatedRenderDigestReader struct {
 Source DelegatedRenderEvidenceReader
 Digest string
}

func (s delegatedRenderDigestReader) ReadDelegatedRenderEvidence(ctx context.Context,
 task CanonicalSigningTask) (SigningRenderEvidence,error) {
 var zero SigningRenderEvidence
 if s.Source==nil||!digest(s.Digest){return zero,ErrInvalid}
 evidence,err:=s.Source.ReadDelegatedRenderEvidence(ctx,task)
 if err!=nil{return zero,err}
 actual,err:=signingRenderEvidenceDigest(evidence)
 if err!=nil||actual!=s.Digest{return zero,ErrInvalid}
 return evidence,nil
}

func (s BoundDelegatedRenderReviewedCausalSource) ReadDelegatedRenderReviewed(
 ctx context.Context,ready ReadySigningExport,task CanonicalSigningTask) (signingRenderReviewedCausalReceipt,error) {
 var zero signingRenderReviewedCausalReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Reviews==nil{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 causal,err:=s.Causal.ReadDelegatedRenderCausal(ctx,ready,owned)
 if err!=nil{return zero,err}
 outputs,err:=ResolveDelegatedRenderOutputs(ctx,delegatedRenderDigestReader{
  Source:s.Causal.Evidence,Digest:causal.renderDigest},s.Reviews,owned)
 if err!=nil{return zero,err}
 current,err:=s.Causal.ReadDelegatedRenderCausal(ctx,ready,owned)
 if err!=nil{return zero,err}
 if causal.stamp!=current.stamp{return zero,ErrInvalid}
 before,err:=canonical.Hash([]any{causal.pixels.inputs,causal.pixels.producers,outputs})
 if err!=nil{return zero,err}
 finalOutputs,err:=ResolveDelegatedRenderOutputs(ctx,delegatedRenderDigestReader{
  Source:s.Causal.Evidence,Digest:current.renderDigest},s.Reviews,owned)
 if err!=nil{return zero,err}
 after,err:=canonical.Hash([]any{current.pixels.inputs,current.pixels.producers,finalOutputs})
 if err!=nil||before!=after{return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return signingRenderReviewedCausalReceipt{causal:current,outputs:finalOutputs},nil
}

var _ DelegatedRenderReviewedReader=BoundDelegatedRenderReviewedCausalSource{}
