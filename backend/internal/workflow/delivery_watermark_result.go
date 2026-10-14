package workflow

import (
 "bytes"
 "encoding/json"
 "math"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// ValidateDeliveryWatermarkResult checks manifest/report value evidence against
// independently reconstructed pinned stage. Physical pixel/alpha verification,
// producer attempt/fence and current persisted pin authority remain mandatory.
func ValidateDeliveryWatermarkResult(node CompiledDownloadNode,pin delivery.FrozenWatermarkSource,scope delivery.WatermarkPinScope,configRaw,sourceRaw []byte,outputRef review.ArtifactRef,outputRaw []byte,reportRef review.ArtifactRef,reportRaw []byte)(review.WorkingImage,error){
 fail:=func()(review.WorkingImage,error){return review.WorkingImage{},domain.NewError("DELIVERY_DERIVATIVE_MISMATCH",409)}
 if node.Operation!="derivative.watermark"||node.Version!="1"||node.ResourceClass!="cpu"||len(node.Inputs)<5||len(outputRaw)>1<<20||len(reportRaw)>1<<20||!validExpansionRef(outputRef)||!validExpansionRef(reportRef)||renderDerivativeBytesSHA(outputRaw)!=outputRef.SHA256||renderDerivativeBytesSHA(reportRaw)!=reportRef.SHA256{return fail()}
 stage,err:=pin.CompileStage(scope,configRaw,v2input.Ref{ArtifactID:node.Inputs[0].ID,SHA256:node.Inputs[0].SHA256},sourceRaw)
 if err!=nil||!bytes.Equal(stage.Params,node.Params)||len(stage.Inputs)!=len(node.Inputs){return fail()}
 forbidden:=map[uuid.UUID]bool{}
 for n,ref:=range stage.Inputs{if node.Inputs[n].ID!=ref.ArtifactID||node.Inputs[n].SHA256!=ref.SHA256{return fail()};forbidden[ref.ArtifactID]=true}
 source,err:=review.ParseWorkingImage(sourceRaw);if err!=nil{return fail()}
 output,err:=review.ParseWorkingImage(outputRaw);if err!=nil{return fail()}
 exact,err:=canonical.Marshal(output)
 if err!=nil||!bytes.Equal(exact,outputRaw)||output.Canvas!=source.Canvas||output.Color!=source.Color||output.DType!=source.DType||!bytes.Equal(output.TransformChain,source.TransformChain)||(output.Alpha==nil)!=(source.Alpha==nil){return fail()}
 resultRefs:=[]review.ArtifactRef{outputRef,reportRef,output.Pixels};if output.Alpha!=nil{resultRefs=append(resultRefs,*output.Alpha)}
 for _,ref:=range resultRefs{if !validExpansionRef(ref)||forbidden[ref.ID]{return fail()};forbidden[ref.ID]=true}
 var params delivery.WatermarkParams
 if json.Unmarshal(stage.Params,&params)!=nil{return fail()}
 var report DeliveryDerivativeReport
 if json.Unmarshal(reportRaw,&report)!=nil{return fail()}
 exact,err=canonical.Marshal(report)
 if err!=nil||!bytes.Equal(exact,reportRaw)||report.Image!=node.Inputs[0]||report.Operation!="watermark"||report.Applied!=(params.Opacity>0)||report.Width!=source.Canvas.Width||report.Height!=source.Canvas.Height||math.IsNaN(report.MaxChannelDelta)||math.IsInf(report.MaxChannelDelta,0)||report.MaxChannelDelta<0||report.MaxChannelDelta>1{return fail()}
 if params.Opacity==0{if report.MaxChannelDelta!=0||len(report.Warnings)!=1||report.Warnings[0]!="zero_opacity"{return fail()}}else if len(report.Warnings)!=0{return fail()}
 return output,nil
}
