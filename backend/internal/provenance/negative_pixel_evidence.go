package provenance

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type SigningNegativeMaskCounter interface{
 CountSigningNegativeMask(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask,v2input.Ref,review.MaskRef)(int64,error)
}

// Private invocation-local evidence is not complete numerical replay or human
// acceptance. Physical RGB array verification and ancestry remain mandatory.
type SigningNegativePixelEvidence struct{
 taskDigest string
 root v2input.Ref
 params v2optical.NegativeParams
 result v2draft.ImageResultDocument
 source,image review.WorkingImage
 changedPixels int64
}

func ReadSigningNegativePixelEvidence(ctx context.Context,adoption SigningNegativeAdoptionReader,documents SigningTaskDocumentReader,counter SigningNegativeMaskCounter,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask)(SigningNegativePixelEvidence,error){
 var zero SigningNegativePixelEvidence
 if err:=ctx.Err();err!=nil{return zero,err}
 if documents==nil||counter==nil{return zero,ErrInvalid}
 root,result,err:=ReadSigningNegativeResult(ctx,adoption,documents,who,ready,task);if err!=nil{return zero,err}
 type witness struct{pin v2input.Ref;raw []byte;maximum int}
 witnesses:=[]witness{}
 read:=func(pin v2input.Ref,maximum int)([]byte,error){
  raw,err:=documents.ReadSigningTaskDocument(ctx,who,ready,task,pin,maximum);if err!=nil{return nil,err}
  raw=append([]byte(nil),raw...);if !pinned(raw,pin,maximum){return nil,ErrInvalid}
  witnesses=append(witnesses,witness{pin,raw,maximum});return raw,nil
 }
 paramsRaw,err:=read(task.Params,65536);if err!=nil{return zero,err}
 params,err:=v2optical.ParseNegativeParams(paramsRaw);if err!=nil{return zero,err}
 sourceRaw,err:=read(params.Base.Source,65536);if err!=nil{return zero,err}
 source,err:=v2optical.VerifyNegativeWorkingImage(params,sourceRaw);if err!=nil{return zero,err}
 imageRaw,err:=read(result.WorkingImage,1<<20);if err!=nil{return zero,err}
 image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return zero,err}
 expected:=source;expected.Pixels=image.Pixels
 expectedRaw,err:=canonical.Marshal(expected)
 if err!=nil||!bytes.Equal(expectedRaw,imageRaw)||image.Pixels.ID==result.WorkingImage.ArtifactID||image.Pixels.ID==root.ArtifactID||image.Pixels.ID==params.Calibration.ArtifactID||image.Pixels.ID==params.Base.Source.ArtifactID||source.Alpha!=nil&&image.Pixels.ID==source.Alpha.ID{return zero,ErrInvalid}
 changed,err:=ReadSigningNegativeMask(ctx,documents,who,ready,task,image,result.ChangedMask,"changed");if err!=nil{return zero,err}
 maskRaw,err:=canonical.Marshal(changed);if err!=nil{return zero,err}
 witnesses=append(witnesses,witness{result.ChangedMask,maskRaw,1<<20})
 count,err:=counter.CountSigningNegativeMask(ctx,who,ready,task,result.ChangedMask,changed);if err!=nil{return zero,err}
 if count<0||count>image.Canvas.Width*image.Canvas.Height||params.Strength==0&&count!=0{return zero,ErrInvalid}
 for _,witness:=range witnesses{
  current,err:=documents.ReadSigningTaskDocument(ctx,who,ready,task,witness.pin,witness.maximum)
  if err!=nil{return zero,err};if !bytes.Equal(current,witness.raw){return zero,ErrInvalid}
 }
 currentRoot,currentResult,err:=ReadSigningNegativeResult(ctx,adoption,documents,who,ready,task);if err!=nil{return zero,err}
 if currentRoot!=root||currentResult.WorkingImage!=result.WorkingImage||currentResult.ChangedMask!=result.ChangedMask||currentResult.Report!=result.Report||currentResult.GeneratedMask!=nil{return zero,ErrInvalid}
 taskDigest,err:=canonical.Hash(task);if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return SigningNegativePixelEvidence{taskDigest:taskDigest,root:root,params:params,result:result,source:source,image:image,changedPixels:count},nil
}
