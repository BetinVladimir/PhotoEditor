package delivery

import (
 "bytes"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// WatermarkStage contains exact worker-ordered roots. Compilation establishes
// value binding only: caller must resolve physical arrays/ACL and adopted source.
type WatermarkStage struct {Params []byte;ParamsSHA string;Inputs []v2input.Ref;ImagePixels int64;WatermarkPixels int64;ImageHasAlpha bool}
func(p FrozenWatermarkSource)CompileStage(scope WatermarkPinScope,configRaw []byte,imageRef v2input.Ref,imageRaw []byte)(WatermarkStage,error){
 var zero WatermarkStage
 owned,err:=DecodeWatermarkSourcePin(p.JCS(),p.SHA256(),scope,configRaw);if err!=nil{return zero,err}
 params,pin,err:=owned.CompileParams(scope,configRaw,imageRef,imageRaw);if err!=nil{return zero,err}
 var envelope watermarkPinEnvelope
 if json.Unmarshal(owned.body,&envelope)!=nil{return zero,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return zero,err}
 exact,err:=canonical.Marshal(image);if err!=nil||!bytes.Equal(exact,imageRaw){return zero,v2input.ErrInvalid}
 mark,err:=review.ParseWorkingImage([]byte(envelope.Manifest));if err!=nil{return zero,err}
 inputs:=[]v2input.Ref{imageRef,{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256}}
 if image.Alpha!=nil{inputs=append(inputs,v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256})}
 inputs=append(inputs,envelope.Binding.Source,v2input.Ref{ArtifactID:mark.Pixels.ID,SHA256:mark.Pixels.SHA256},v2input.Ref{ArtifactID:mark.Alpha.ID,SHA256:mark.Alpha.SHA256})
 seen:=map[uuid.UUID]bool{}
 for _,ref:=range inputs{if ref.ArtifactID==uuid.Nil||!shaPattern.MatchString(ref.SHA256)||seen[ref.ArtifactID]{return zero,v2input.ErrInvalid};seen[ref.ArtifactID]=true}
 return WatermarkStage{Params:params,ParamsSHA:pin,Inputs:inputs,ImagePixels:image.Canvas.Width*image.Canvas.Height,WatermarkPixels:mark.Canvas.Width*mark.Canvas.Height,ImageHasAlpha:image.Alpha!=nil},nil
}
