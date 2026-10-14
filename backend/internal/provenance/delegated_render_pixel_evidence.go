package provenance

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type DelegatedRenderMaskCounter interface {
 CountDelegatedRenderMask(context.Context,CanonicalSigningTask,
  v2input.Ref,review.MaskRef)(int64,error)
}

func ReadDelegatedRenderPixelEvidence(ctx context.Context,
 adopted DelegatedAdoptedResultPort,documents DelegatedTaskDocuments,
 counter DelegatedRenderMaskCounter,task CanonicalSigningTask)(SigningRenderPixelEvidence,error){
 var zero SigningRenderPixelEvidence
 if err:=ctx.Err();err!=nil{return zero,err}
 if adopted==nil||documents==nil||counter==nil||
  task.Operation!="render.compose"||task.Version!="1"{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 recipe,err:=ReadDelegatedRenderRecipe(ctx,documents,owned)
 if err!=nil{return zero,err}
 root,result,err:=adopted.ReadDelegatedAdoptedResult(ctx,owned)
 if err!=nil{return zero,err}
 readImage:=func(reference v2input.Ref)(review.WorkingImage,[]byte,error){
  isolated,err:=ownSigningProducerTask(owned)
  if err!=nil{return review.WorkingImage{},nil,err}
  raw,err:=documents.ReadDelegatedTaskDocument(ctx,isolated,reference,1<<20)
  if err!=nil{return review.WorkingImage{},nil,err}
  if !pinned(raw,reference,1<<20){return review.WorkingImage{},nil,ErrInvalid}
  body:=append([]byte(nil),raw...)
  image,err:=review.ParseWorkingImage(body)
  return image,body,err
 }
 image,imageRaw,err:=readImage(result.WorkingImage)
 if err!=nil{return zero,err}
 source,sourceRaw,err:=readImage(recipe.Scalars.Source.Source)
 if err!=nil{return zero,err}
 canvasHash,err:=canonical.Hash(source.Canvas)
 if err!=nil||canvasHash!=recipe.Scalars.Source.CanvasHash{return zero,ErrInvalid}
 if image.DType!="float32"||source.DType!=image.DType||
  source.Canvas!=image.Canvas||source.Color!=image.Color||
  !bytes.Equal(source.TransformChain,image.TransformChain){return zero,ErrInvalid}
 changed,err:=ReadDelegatedRenderMask(ctx,documents,owned,image,
  result.ChangedMask,"changed")
 if err!=nil{return zero,err}
 changedCount,err:=counter.CountDelegatedRenderMask(ctx,owned,result.ChangedMask,changed)
 if err!=nil{return zero,err}
 generatedCount:=int64(0)
 if result.GeneratedMask!=nil{
  generated,err:=ReadDelegatedRenderMask(ctx,documents,owned,image,
   *result.GeneratedMask,"generated")
  if err!=nil{return zero,err}
  generatedCount,err=counter.CountDelegatedRenderMask(ctx,owned,
   *result.GeneratedMask,generated)
  if err!=nil{return zero,err}
 }
 limit:=image.Canvas.Width*image.Canvas.Height
 if changedCount<0||changedCount>limit||generatedCount<0||
  generatedCount>limit{return zero,ErrInvalid}
 _,currentImage,err:=readImage(result.WorkingImage)
 if err!=nil{return zero,err}
 _,currentSource,err:=readImage(recipe.Scalars.Source.Source)
 if err!=nil{return zero,err}
 if !bytes.Equal(imageRaw,currentImage)||!bytes.Equal(sourceRaw,currentSource){return zero,ErrInvalid}
 currentRoot,currentResult,err:=adopted.ReadDelegatedAdoptedResult(ctx,owned)
 if err!=nil{return zero,err}
 if currentRoot!=root||currentResult.WorkingImage!=result.WorkingImage||
  currentResult.ChangedMask!=result.ChangedMask||
  (currentResult.GeneratedMask==nil)!=(result.GeneratedMask==nil)||
  currentResult.GeneratedMask!=nil&&*currentResult.GeneratedMask!=*result.GeneratedMask{
  return zero,ErrInvalid
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 return SigningRenderPixelEvidence{root:root,recipe:recipe,result:result,
  image:image,changedPixels:changedCount,generatedPixels:generatedCount},nil
}
