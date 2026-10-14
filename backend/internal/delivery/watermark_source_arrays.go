package delivery

import (
 "context"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// VerifyWatermarkSourceArrays streams both immutable NPY objects with bounded
// memory. Caller must supply independently authorized descriptors and keep SQL
// transactions closed during IO; this does not itself establish access rights.
func VerifyWatermarkSourceArrays(ctx context.Context,objects v2input.SessionAnalysisObjectReader,evidence WatermarkSourceEvidence)error{
 if err:=ctx.Err();err!=nil{return err}
 if objects==nil||len(evidence.Manifest)>1<<20||digest(evidence.Manifest)!=evidence.Source.Ref.SHA256||len(evidence.Arrays)!=2{return v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(evidence.Manifest)
 if err!=nil||image.Alpha==nil||image.DType!="float32"||image.Canvas.Width<1||image.Canvas.Height<1||image.Canvas.Width>100000||image.Canvas.Height>100000||image.Canvas.Width*image.Canvas.Height>200000000{return v2input.ErrInvalid}
 expected:=[]v2input.Ref{{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256}}
 // Validate both descriptors before opening the first binary object.
 for n,array:=range evidence.Arrays{
  contract:="WorkingPixels";minimum:=image.Canvas.Width*image.Canvas.Height*12;maximum:=minimum+8192
  if n==1{contract="WorkingAlpha";minimum=image.Canvas.Width*image.Canvas.Height*2;maximum=image.Canvas.Width*image.Canvas.Height*4+8192}
  if array.Contract!=contract||array.Ref!=expected[n]||array.Object.SHA256!=array.Ref.SHA256||array.Object.Key==""||!objectstore.ValidInterchangeVersionID(array.Object.VersionID)||array.Object.Bytes<minimum||array.Object.Bytes>maximum||(array.Object.ContentType!="application/octet-stream"&&array.Object.ContentType!="application/x-npy"){return v2input.ErrInvalid}
 }
 for n,array:=range evidence.Arrays{
  if err=ctx.Err();err!=nil{return err}
  reader,openErr:=objects.Open(ctx,array.Object);if openErr!=nil{return openErr};if reader==nil{return v2input.ErrInvalid}
  if n==0{err=masknpy.VerifyFloat32ArrayNPY(ctx,reader,array.Object.Bytes,array.Ref.SHA256,image.Canvas.Width,image.Canvas.Height,3,false)}else{err=masknpy.VerifyAlphaNPY(ctx,reader,array.Object.Bytes,array.Ref.SHA256,image.Canvas.Width,image.Canvas.Height)}
  closeErr:=reader.Close();if err!=nil{return err};if closeErr!=nil{return closeErr}
 }
 return ctx.Err()
}
