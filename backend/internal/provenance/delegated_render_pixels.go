package provenance

import (
 "context"

 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type DelegatedRenderPixelsCatalog interface {
 LookupDelegatedRenderPixels(context.Context,CanonicalSigningTask,
  v2input.Ref,review.WorkingImage)(objectstore.Object,error)
}

type BoundDelegatedRenderPixelsVerifier struct {
 Catalog DelegatedRenderPixelsCatalog
 Objects SigningObjects
}

func(s BoundDelegatedRenderPixelsVerifier)Verify(ctx context.Context,
 task CanonicalSigningTask,pixels SigningRenderPixelEvidence)error{
 if err:=ctx.Err();err!=nil{return err}
 image:=pixels.image
 if s.Catalog==nil||s.Objects==nil||task.Operation!="render.compose"||
  task.Version!="1"||normalizeCanonicalSigningTask(&task)!=nil||
  !ref(pixels.result.WorkingImage)||image.DType!="float32"||
  image.Canvas.Width<1||image.Canvas.Height<1||image.Canvas.Width>200000000||
  image.Canvas.Height>200000000/image.Canvas.Width{return ErrInvalid}
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return err}
 object,err:=s.Catalog.LookupDelegatedRenderPixels(ctx,owned,
  pixels.result.WorkingImage,image)
 if err!=nil{return err}
 size:=12*image.Canvas.Width*image.Canvas.Height
 if object.Key==""||object.VersionID==""||object.VersionID=="null"||
  object.SHA256!=image.Pixels.SHA256||object.ContentType!="application/x-npy"||
  object.Bytes<size||object.Bytes>size+65536{return ErrInvalid}
 stream,err:=s.Objects.Open(ctx,object)
 if err!=nil{if stream!=nil{_ = stream.Close()};return err}
 if stream==nil{return ErrInvalid}
 verifyErr:=masknpy.VerifyFloat32ArrayNPY(ctx,stream,object.Bytes,
  object.SHA256,image.Canvas.Width,image.Canvas.Height,3,false)
 closeErr:=stream.Close()
 if err:=ctx.Err();err!=nil{return err}
 if verifyErr!=nil{return verifyErr}
 if closeErr!=nil{return closeErr}
 current,err:=s.Catalog.LookupDelegatedRenderPixels(ctx,owned,
  pixels.result.WorkingImage,image)
 if err!=nil{return err}
 if current!=object{return ErrInvalid}
 return ctx.Err()
}
