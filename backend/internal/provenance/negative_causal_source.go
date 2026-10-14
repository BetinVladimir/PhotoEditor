package provenance

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type SigningNegativeEvidenceReader interface{
 ReadSigningNegativeEvidence(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask)(SigningNegativeEvidence,error)
}

type signingNegativePixelAncestry struct{input v2input.Ref;producer uuid.UUID}

func ResolveSigningNegativePixelAncestry(ctx context.Context,producers SigningImageProducer,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask,evidence SigningNegativeEvidence)(signingNegativePixelAncestry,error){
 var zero signingNegativePixelAncestry
 if err:=ctx.Err();err!=nil{return zero,err}
 taskSHA,err:=canonical.Hash(task)
 input:=evidence.pixels.params.Base.Source
 if err!=nil||producers==nil||task.Task==uuid.Nil||task.Operation!="optical.negative"||task.Version!="1"||taskSHA!=evidence.taskDigest||taskSHA!=evidence.pixels.taskDigest||!ref(input){return zero,ErrInvalid}
 producer,err:=producers.ResolveSigningImageProducer(ctx,who,ready,input);if err!=nil{return zero,err}
 if producer==uuid.Nil||producer==task.Task{return zero,ErrInvalid}
 current,err:=producers.ResolveSigningImageProducer(ctx,who,ready,input);if err!=nil{return zero,err}
 if current!=producer{return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return signingNegativePixelAncestry{input:input,producer:producer},nil
}

type BoundSigningNegativeCausalSource struct{
 Evidence SigningNegativeEvidenceReader
 Producers SigningImageProducer
 Documents SigningTaskDocumentReader
}
type signingNegativeCausalReceipt struct{
 evidence SigningNegativeEvidence
 pixels signingNegativePixelAncestry
 stamp string
 entries []v2input.Entry
}

// Join the exact input-to-producer edge to a stable physical/human evidence
// receipt. This does not grant parameter provenance or create a signing node.
func(s BoundSigningNegativeCausalSource)read(ctx context.Context,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask)(signingNegativeCausalReceipt,error){
 var zero signingNegativeCausalReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Evidence==nil||s.Producers==nil||s.Documents==nil{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task);if err!=nil{return zero,err}
 read:=func()(SigningNegativeEvidence,error){
  isolated,err:=ownSigningProducerTask(owned);if err!=nil{return SigningNegativeEvidence{},err}
  evidence,err:=s.Evidence.ReadSigningNegativeEvidence(ctx,who,ready,isolated);if err!=nil{return SigningNegativeEvidence{},err}
  stamp,err:=compileSigningNegativeEvidenceStamp(owned,evidence)
  if err!=nil||stamp!=evidence.stamp{return SigningNegativeEvidence{},ErrInvalid}
  return evidence,nil
 }
 evidence,err:=read();if err!=nil{return zero,err};stamp:=evidence.stamp
 entries,err:=CompileSigningNegativeInputInventory(owned,evidence);if err!=nil{return zero,err}
 if err:=VerifySigningNegativeTransport(ctx,s.Documents,who,ready,owned,entries);err!=nil{return zero,err}
 pixels,err:=ResolveSigningNegativePixelAncestry(ctx,s.Producers,who,ready,owned,evidence);if err!=nil{return zero,err}
 // External authority adapters may reuse mutable evidence storage.
 currentStamp,err:=compileSigningNegativeEvidenceStamp(owned,evidence);if err!=nil||currentStamp!=stamp{return zero,ErrInvalid}
 current,err:=read();if err!=nil{return zero,err};if current.stamp!=stamp{return zero,ErrInvalid}
 finalPixels,err:=ResolveSigningNegativePixelAncestry(ctx,s.Producers,who,ready,owned,current);if err!=nil{return zero,err}
 if finalPixels!=pixels{return zero,ErrInvalid}
 finalStamp,err:=compileSigningNegativeEvidenceStamp(owned,current);if err!=nil||finalStamp!=stamp{return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 if err:=VerifySigningNegativeTransport(ctx,s.Documents,who,ready,owned,entries);err!=nil{return zero,err}
 finalStamp,err=compileSigningNegativeEvidenceStamp(owned,current);if err!=nil||finalStamp!=stamp{return zero,ErrInvalid}
 causalStamp,err:=canonical.Hash([]any{stamp,pixels.input,pixels.producer,entries});if err!=nil{return zero,err}
 return signingNegativeCausalReceipt{evidence:current,pixels:finalPixels,stamp:causalStamp,entries:entries},nil
}

var _ SigningNegativeEvidenceReader=BoundSigningNegativeEvidenceReader{}
