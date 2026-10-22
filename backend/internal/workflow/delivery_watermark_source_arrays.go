package workflow

import (
 "context"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// verifyInitialWatermarkSourceArrays checks physical source bytes before Params
// publication. Descriptors must come from the current SQL-authorized inventory;
// this verifier cannot make an arbitrary descriptor authoritative.
func verifyInitialWatermarkSourceArrays(ctx context.Context,objects v2input.SessionAnalysisObjectReader,ref v2input.Ref,raw []byte,arrays []delivery.WatermarkArrayObject)error{
 return verifyExportWorkingImageArrays(ctx,objects,ref,raw,arrays)
}

// verifyExportWorkingImageArrays accepts only independently authorized exact refs.
// It preserves canonical image/ICC/alpha representations and verifies all actual NPY bytes.
func verifyExportWorkingImageArrays(ctx context.Context,objects v2input.SessionAnalysisObjectReader,ref v2input.Ref,raw []byte,arrays []delivery.WatermarkArrayObject)error{
 if err:=ctx.Err();err!=nil{return err}
 if objects==nil||len(raw)<2||len(raw)>1<<20||renderDerivativeBytesSHA(raw)!=ref.SHA256{return v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(raw)
 if err!=nil||image.DType!="float32"||image.Color.Encoding!="gamma24"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Canvas.Width<1||image.Canvas.Height<1||image.Canvas.Width>100000||image.Canvas.Height>100000||image.Canvas.Width*image.Canvas.Height>200000000{return v2input.ErrInvalid}
 expected:=[]v2input.Ref{{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256}}
 if image.Alpha!=nil{expected=append(expected,v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256})}
 if len(arrays)!=len(expected){return v2input.ErrInvalid}
 for n,array:=range arrays{
  contract:="WorkingPixels";minimum:=image.Canvas.Width*image.Canvas.Height*12;maximum:=minimum+8192
  if n==1{contract="WorkingAlpha";minimum=image.Canvas.Width*image.Canvas.Height*2;maximum=image.Canvas.Width*image.Canvas.Height*4+8192}
  if array.Contract!=contract||array.Ref!=expected[n]||array.Object.SHA256!=array.Ref.SHA256||array.Object.Key==""||!objectstore.ValidInterchangeVersionID(array.Object.VersionID)||array.Object.Bytes<minimum||array.Object.Bytes>maximum||(array.Object.ContentType!="application/octet-stream"&&array.Object.ContentType!="application/x-npy"){return v2input.ErrInvalid}
 }
 for n,array:=range arrays{
  if err=ctx.Err();err!=nil{return err}
  reader,openErr:=objects.Open(ctx,array.Object);if openErr!=nil{return physicalDeliveryFailure(array.Ref,array.Object.VersionID,openErr)};if reader==nil{return v2input.ErrInvalid}
  if n==0{err=masknpy.VerifyFloat32ArrayNPY(ctx,reader,array.Object.Bytes,array.Ref.SHA256,image.Canvas.Width,image.Canvas.Height,3,false)}else{err=masknpy.VerifyAlphaNPY(ctx,reader,array.Object.Bytes,array.Ref.SHA256,image.Canvas.Width,image.Canvas.Height)}
  closeErr:=reader.Close();if err!=nil{return physicalDeliveryFailure(array.Ref,array.Object.VersionID,err)};if closeErr!=nil{return physicalDeliveryFailure(array.Ref,array.Object.VersionID,closeErr)}
 }
 return ctx.Err()
}
