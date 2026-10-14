package delivery

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type WatermarkArrayObject struct {Contract string;Ref v2input.Ref;Object objectstore.Object}
// Manifest bytes are physically verified. Array descriptors are independently
// catalog-authorized; workers must still verify NPY bytes/shape/values.
type WatermarkSourceEvidence struct {Source WatermarkSource;Binding v2input.SessionVersionBinding;Manifest []byte;Arrays []WatermarkArrayObject}
func LoadWatermarkSourceEvidence(ctx context.Context,who auth.Identity,tid uuid.UUID,source WatermarkSource,
 objects v2input.SessionAnalysisObjectReader,catalog v2draft.ArtifactCatalog)(WatermarkSourceEvidence,error){
 var zero WatermarkSourceEvidence
 if err:=ctx.Err();err!=nil{return zero,err}
 if tid==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||source.Asset==uuid.Nil||source.Version==uuid.Nil||objects==nil||catalog==nil{return zero,v2input.ErrInvalid}
 object,err:=catalog.Lookup(ctx,who,tid,v2draft.LayerRef{Ref:source.Ref,Contract:"WorkingImage"});if err!=nil{return zero,err}
 if object.Key!=source.Object.Key||object.VersionID!=source.Object.VersionID||object.Bytes!=source.Object.Bytes||object.SHA256!=source.Ref.SHA256||object.Key==""||!objectstore.ValidInterchangeVersionID(object.VersionID){return zero,v2input.ErrInvalid}
 image,err:=v2input.ReadVerifiedWorkingImage(ctx,objects,object,source.Ref);if err!=nil{return zero,err}
 if image.Alpha==nil||image.Color.Encoding!="gamma24"{return zero,v2input.ErrInvalid}
 if image.Canvas.Width<1||image.Canvas.Height<1||image.Canvas.Width>100000||image.Canvas.Height>100000||image.Canvas.Width*image.Canvas.Height>200000000{return zero,v2input.ErrInvalid}
 raw,err:=canonical.Marshal(image);if err!=nil||digest(raw)!=source.Ref.SHA256{return zero,v2input.ErrInvalid}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return zero,err}
 binding:=v2input.SessionVersionBinding{AssetVersionID:source.Version,Source:source.Ref,CanvasHash:canvas}
 refs:=[]struct{contract string;ref v2input.Ref;channels int64}{
  {"WorkingPixels",v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},3},
  {"WorkingAlpha",v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256},1},
 }
 if refs[0].ref.ArtifactID==refs[1].ref.ArtifactID||refs[0].ref.ArtifactID==source.Ref.ArtifactID||refs[1].ref.ArtifactID==source.Ref.ArtifactID{return zero,v2input.ErrInvalid}
 arrays:=make([]WatermarkArrayObject,0,2)
 for _,entry:=range refs{
  if err=ctx.Err();err!=nil{return zero,err}
  array,lookupErr:=catalog.Lookup(ctx,who,tid,v2draft.LayerRef{Ref:entry.ref,Contract:entry.contract,ParentRef:source.Ref,CanvasHash:canvas,ExpectedBinding:&binding})
  if lookupErr!=nil{return zero,lookupErr}
  expected:=image.Canvas.Width*image.Canvas.Height*entry.channels*4
  minimum:=expected;if entry.contract=="WorkingAlpha"{minimum=expected/2} // Worker also supports uint16 alpha.
  if (array.ContentType!="application/octet-stream"&&array.ContentType!="application/x-npy")||array.SHA256!=entry.ref.SHA256||array.Key==""||!objectstore.ValidInterchangeVersionID(array.VersionID)||array.Bytes<minimum||array.Bytes>expected+8192{return zero,v2input.ErrInvalid}
  arrays=append(arrays,WatermarkArrayObject{entry.contract,entry.ref,array})
 }
 if err=ctx.Err();err!=nil{return zero,err}
 return WatermarkSourceEvidence{source,binding,raw,arrays},nil
}
