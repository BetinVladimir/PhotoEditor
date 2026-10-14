package provenance

import (
 "context"

 "photoeditor/backend/internal/canonical"
)

type DelegatedRenderEvidenceReader interface {
 ReadDelegatedRenderEvidence(context.Context, CanonicalSigningTask) (SigningRenderEvidence,error)
}

type DelegatedRenderClosureReader interface {
 ReadDelegatedRenderClosure(context.Context, CanonicalSigningTask) (SigningRenderClosureEvidence,error)
}

type DelegatedRenderCausalReader interface {
 ReadDelegatedRenderCausal(context.Context,ReadySigningExport,
  CanonicalSigningTask)(signingRenderCausalReceipt,error)
}

type BoundDelegatedRenderCausalSource struct {
 Evidence DelegatedRenderEvidenceReader
 Closure DelegatedRenderClosureReader
 Producers DelegatedImageProducer
}

// ReadDelegatedRenderCausal joins independently checked render evidence and
// frozen input closure to a stable input-to-producer ancestry map.
func (s BoundDelegatedRenderCausalSource) ReadDelegatedRenderCausal(ctx context.Context,
 _ ReadySigningExport, task CanonicalSigningTask) (signingRenderCausalReceipt,error) {
 var zero signingRenderCausalReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Evidence==nil||s.Closure==nil||s.Producers==nil{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 read:=func()(SigningRenderCausalInventory,string,string,error){
  if err:=ctx.Err();err!=nil{return SigningRenderCausalInventory{},"","",err}
  isolated,err:=ownSigningProducerTask(owned)
  if err!=nil{return SigningRenderCausalInventory{},"","",err}
  render,err:=s.Evidence.ReadDelegatedRenderEvidence(ctx,isolated)
  if err!=nil{return SigningRenderCausalInventory{},"","",err}
  before,err:=signingRenderEvidenceDigest(render)
  if err!=nil{return SigningRenderCausalInventory{},"","",err}
  isolated,err=ownSigningProducerTask(owned)
  if err!=nil{return SigningRenderCausalInventory{},"","",err}
  closure,err:=s.Closure.ReadDelegatedRenderClosure(ctx,isolated)
  if err!=nil{return SigningRenderCausalInventory{},"","",err}
  after,err:=signingRenderEvidenceDigest(render)
  if err!=nil||before!=after{return SigningRenderCausalInventory{},"","",ErrInvalid}
  inventory,err:=CompileSigningRenderCausalInventory(owned,render,closure)
  if err!=nil{return SigningRenderCausalInventory{},"","",err}
  stamp,err:=canonical.Hash([]any{before,closure.taskDigest,closure.inventory})
  return inventory,stamp,before,err
 }
 inventory,stamp,renderDigest,err:=read()
 if err!=nil{return zero,err}
 pixels,err:=ResolveDelegatedRenderPixelAncestry(ctx,s.Producers,owned,inventory)
 if err!=nil{return zero,err}
 currentInventory,current,secondDigest,err:=read()
 if err!=nil{return zero,err}
 if stamp!=current||renderDigest!=secondDigest{return zero,ErrInvalid}
 finalPixels,err:=ResolveDelegatedRenderPixelAncestry(ctx,s.Producers,owned,currentInventory)
 if err!=nil{return zero,err}
 if len(pixels.inputs)!=len(finalPixels.inputs)||
  len(pixels.producers)!=len(finalPixels.producers){return zero,ErrInvalid}
 for i:=range pixels.inputs{
  if pixels.inputs[i]!=finalPixels.inputs[i]||
   pixels.producers[i]!=finalPixels.producers[i]{return zero,ErrInvalid}
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 return signingRenderCausalReceipt{inventory:inventory,pixels:pixels,
  stamp:stamp,renderDigest:renderDigest},nil
}

var _ DelegatedRenderCausalReader=BoundDelegatedRenderCausalSource{}
