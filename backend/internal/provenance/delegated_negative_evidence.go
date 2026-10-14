package provenance

import (
 "context"

 "photoeditor/backend/internal/canonical"
)

type BoundDelegatedNegativeEvidenceReader struct {
 Adopted DelegatedAdoptedResultPort
 Documents DelegatedTaskDocuments
 Counter DelegatedNegativeMaskCounter
 SourceArrays BoundDelegatedNegativeSourceArraysVerifier
 Pixels BoundDelegatedNegativePixelsVerifier
 Qualification BoundDelegatedNegativeQualificationContent
 Reviews DelegatedNegativeReviewPort
}

func (s BoundDelegatedNegativeEvidenceReader) ReadDelegatedNegativeEvidence(
 ctx context.Context, task CanonicalSigningTask) (SigningNegativeEvidence,error) {
 var zero SigningNegativeEvidence
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Adopted==nil||s.Documents==nil||s.Counter==nil||s.Reviews==nil||
  s.SourceArrays.Catalog==nil||s.SourceArrays.Objects==nil||
  s.Pixels.Catalog==nil||s.Pixels.Objects==nil||
  s.Qualification.Historical==nil||s.Qualification.Documents==nil||
  s.Qualification.Policies==nil{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 if owned.Operation!="optical.negative"||owned.Version!="1"||
  !owned.ReviewRequired{return zero,ErrInvalid}
 read:=func()(SigningNegativeEvidence,error){
  isolated,err:=ownSigningProducerTask(owned)
  if err!=nil{return zero,err}
  pixels,err:=ReadDelegatedNegativePixelEvidence(ctx,s.Adopted,
   s.Documents,s.Counter,isolated)
  if err!=nil{return zero,err}
  before,err:=signingNegativePixelDigest(pixels)
  if err!=nil{return zero,err}
  if err:=s.SourceArrays.Verify(ctx,isolated,pixels);err!=nil{return zero,err}
  if err:=s.Pixels.Verify(ctx,isolated,pixels);err!=nil{return zero,err}
  verified,err:=s.Qualification.Read(ctx,isolated)
  if err!=nil{return zero,err}
  pin,err:=verified.Pin()
  if err!=nil{return zero,err}
  reportRef,report,err:=ReadDelegatedNegativeReport(ctx,s.Adopted,s.Documents,isolated)
  if err!=nil{return zero,err}
  if reportRef!=pixels.result.Report{return zero,ErrInvalid}
  decision,err:=s.Reviews.ReadDelegatedNegativeReview(ctx,isolated,pixels.root,reportRef)
  if err!=nil{return zero,err}
  if decision==nil{return zero,ErrInvalid}
  decisionCopy:=*decision
  after,err:=signingNegativePixelDigest(pixels)
  if err!=nil||before!=after{return zero,ErrInvalid}
  taskSHA,err:=canonical.Hash(isolated)
  if err!=nil||taskSHA!=pixels.taskDigest{return zero,ErrInvalid}
  evidence:=SigningNegativeEvidence{taskDigest:taskSHA,pixels:pixels,
   qualification:pin,review:decisionCopy,report:report}
  stamp,err:=compileSigningNegativeEvidenceStamp(owned,evidence)
  if err!=nil{return zero,err}
  evidence.stamp=stamp
  return evidence,ctx.Err()
 }
 first,err:=read()
 if err!=nil{return zero,err}
 current,err:=read()
 if err!=nil{return zero,err}
 if first.stamp!=current.stamp{return zero,ErrInvalid}
 return current,ctx.Err()
}

var _ DelegatedNegativeEvidenceReader=BoundDelegatedNegativeEvidenceReader{}
