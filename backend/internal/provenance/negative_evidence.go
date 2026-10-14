package provenance

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2optical"
)

// This receipt proves the joined physical result and explicit human decision.
// Parent producer ancestry and graph input accounting are separate mandatory stages.
type SigningNegativeEvidence struct {
 taskDigest string
 pixels SigningNegativePixelEvidence
 qualification v2optical.NegativeQualificationPin
 review SigningNegativeReview
 report v2draft.ImageReportDocument
 stamp string
}

type BoundSigningNegativeEvidenceReader struct {
 Adoption SigningNegativeAdoptionReader
 Documents SigningTaskDocumentReader
 Counter SigningNegativeMaskCounter
 SourceArrays BoundSigningNegativeSourceArraysVerifier
 Pixels BoundSigningNegativePixelsVerifier
 Qualification BoundSigningNegativeQualificationContent
 Reviews SigningNegativeReviewReader
}

func(s BoundSigningNegativeEvidenceReader)ReadSigningNegativeEvidence(ctx context.Context,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask)(SigningNegativeEvidence,error){
 var zero SigningNegativeEvidence
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Adoption==nil||s.Documents==nil||s.Counter==nil||s.Reviews==nil||s.SourceArrays.Catalog==nil||s.SourceArrays.Objects==nil||s.Pixels.Catalog==nil||s.Pixels.Objects==nil||s.Qualification.Historical==nil||s.Qualification.Documents==nil||s.Qualification.Policies==nil{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task);if err!=nil{return zero,err}
 if owned.Operation!="optical.negative"||owned.Version!="1"||!owned.ReviewRequired{return zero,ErrInvalid}
 read:=func()(SigningNegativeEvidence,error){
  isolated,err:=ownSigningProducerTask(owned);if err!=nil{return zero,err}
  pixels,err:=ReadSigningNegativePixelEvidence(ctx,s.Adoption,s.Documents,s.Counter,who,ready,isolated);if err!=nil{return zero,err}
  before,err:=signingNegativePixelDigest(pixels);if err!=nil{return zero,err}
  if err:=s.SourceArrays.Verify(ctx,who,ready,isolated,pixels);err!=nil{return zero,err}
  if err:=s.Pixels.Verify(ctx,who,ready,isolated,pixels);err!=nil{return zero,err}
  verified,err:=s.Qualification.Read(ctx,who,ready,isolated);if err!=nil{return zero,err}
  pin,err:=verified.Pin();if err!=nil{return zero,err}
  reportRef,report,err:=ReadSigningNegativeReport(ctx,s.Adoption,s.Documents,who,ready,isolated);if err!=nil{return zero,err}
  if reportRef!=pixels.result.Report{return zero,ErrInvalid}
  decision,err:=s.Reviews.ReadSigningNegativeReview(ctx,who,ready,isolated,pixels.root,reportRef);if err!=nil{return zero,err}
  if decision==nil{return zero,ErrInvalid};decisionCopy:=*decision
  after,err:=signingNegativePixelDigest(pixels);if err!=nil||before!=after{return zero,ErrInvalid}
  taskSHA,err:=canonical.Hash(isolated);if err!=nil||taskSHA!=pixels.taskDigest{return zero,ErrInvalid}
  evidence:=SigningNegativeEvidence{taskDigest:taskSHA,pixels:pixels,qualification:pin,review:decisionCopy,report:report}
  stamp,err:=compileSigningNegativeEvidenceStamp(owned,evidence);if err!=nil{return zero,err};evidence.stamp=stamp
  return evidence,ctx.Err()
 }
 first,err:=read();if err!=nil{return zero,err}
 current,err:=read();if err!=nil{return zero,err}
 if first.stamp!=current.stamp{return zero,ErrInvalid}
 return current,ctx.Err()
}

func signingNegativePixelDigest(e SigningNegativePixelEvidence)(string,error){
 return canonical.Hash([]any{e.taskDigest,e.root,e.params,e.result,e.source,e.image,e.changedPixels})
}
func compileSigningNegativeEvidenceStamp(task CanonicalSigningTask,e SigningNegativeEvidence)(string,error){
 taskSHA,err:=canonical.Hash(task)
 if err!=nil||taskSHA!=e.taskDigest||e.pixels.taskDigest!=taskSHA||task.Operation!="optical.negative"||task.Version!="1"||!task.ReviewRequired{return "",ErrInvalid}
 if _,err:=e.qualification.Compile();err!=nil{return "",ErrInvalid}
 if e.qualification.Calibration!=e.pixels.params.Calibration||e.review.ID==uuid.Nil||!e.review.Accepted||e.review.Task!=task.Task||e.review.Result!=task.Result||e.review.Root!=e.pixels.root||e.review.Report!=e.pixels.result.Report{return "",ErrInvalid}
 pixelsSHA,err:=signingNegativePixelDigest(e.pixels);if err!=nil{return "",err}
 return canonical.Hash([]any{taskSHA,pixelsSHA,e.qualification,e.review,e.report})
}
