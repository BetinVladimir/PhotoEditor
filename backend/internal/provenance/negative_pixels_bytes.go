package provenance

import (
 "context"

 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type SigningNegativePixelsCatalog interface{
 LookupSigningNegativePixels(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask,v2input.Ref,review.WorkingImage)(objectstore.Object,error)
}
type BoundSigningNegativePixelsVerifier struct{
 Catalog SigningNegativePixelsCatalog
 Objects SigningObjects
}

// Validate physical output bytes with bounded streaming memory; no full-image
// allocation. This does not independently recompute negative colour conversion.
func(s BoundSigningNegativePixelsVerifier)Verify(ctx context.Context,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask,evidence SigningNegativePixelEvidence)error{
 if err:=ctx.Err();err!=nil{return err}
 digest,err:=canonical.Hash(task)
 if err!=nil||digest!=evidence.taskDigest||s.Catalog==nil||s.Objects==nil||task.Operation!="optical.negative"||task.Version!="1"||!ref(evidence.result.WorkingImage){return ErrInvalid}
 image:=evidence.image
 if image.DType!="float32"||image.Canvas.Width<1||image.Canvas.Height<1||image.Canvas.Width>200000000||image.Canvas.Height>200000000/image.Canvas.Width{return ErrInvalid}
 object,err:=s.Catalog.LookupSigningNegativePixels(ctx,who,ready,task,evidence.result.WorkingImage,image);if err!=nil{return err}
 size:=12*image.Canvas.Width*image.Canvas.Height
 if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.SHA256!=image.Pixels.SHA256||object.ContentType!="application/x-npy"||object.Bytes<size||object.Bytes>size+65536{return ErrInvalid}
 stream,err:=s.Objects.Open(ctx,object)
 if err!=nil{if stream!=nil{_ = stream.Close()};return err};if stream==nil{return ErrInvalid}
 verifyErr:=masknpy.VerifyFloat32ArrayNPY(ctx,stream,object.Bytes,object.SHA256,image.Canvas.Width,image.Canvas.Height,3,false)
 closeErr:=stream.Close()
 if err:=ctx.Err();err!=nil{return err};if verifyErr!=nil{return verifyErr};if closeErr!=nil{return closeErr}
 current,err:=s.Catalog.LookupSigningNegativePixels(ctx,who,ready,task,evidence.result.WorkingImage,image);if err!=nil{return err}
 if current!=object{return ErrInvalid}
 return ctx.Err()
}
