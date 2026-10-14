package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type ExportSeriesMeasureAuthority interface{QualifyExportSeriesMeasure(context.Context,ExportSeriesOriginEvidence,v2input.Entry,[]byte,ExportClosureNode)error}
type NativeExportSeriesMeasureQualifier struct{Reader ExportSeriesMeasureParamsReader;Inputs ExportSeriesMeasureInputAuthority;native *NativeExportStyleValidator}
func NewNativeExportSeriesMeasureQualifier(reader ExportSeriesMeasureParamsReader,inputs ExportSeriesMeasureInputAuthority,scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string)(*NativeExportSeriesMeasureQualifier,error){
 if reader.Source==nil||reader.Objects==nil||inputs==nil{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,exportCompositionStagingValidator{});if err!=nil{return nil,err};native.contract="SeriesMeasureOrigin"
 return &NativeExportSeriesMeasureQualifier{Reader:reader,Inputs:inputs,native:native},nil
}
func(q *NativeExportSeriesMeasureQualifier)QualifyExportSeriesMeasure(ctx context.Context,balance ExportSeriesOriginEvidence,entry v2input.Entry,raw []byte,image ExportClosureNode)error{
 if err:=ctx.Err();err!=nil{return err}
 if q==nil||q.native==nil||q.Reader.Source==nil||q.Reader.Objects==nil||q.Inputs==nil||entry.Contract!="SeriesStats"||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=entry.Ref.SHA256{return v2input.ErrInvalid}
 if image.Entry.Contract!="WorkingImage"||!validImportRef(image.Entry.Ref)||len(image.Body)<2||len(image.Body)>8<<20||rawSHA(image.Body)!=image.Entry.Ref.SHA256{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...);imageBody:=append([]byte(nil),image.Body...)
 var stats struct{Image v2input.Ref `json:"image"`}
 if json.Unmarshal(body,&stats)!=nil||stats.Image!=image.Entry.Ref{return v2input.ErrInvalid}
 if _,err:=review.ParseWorkingImage(imageBody);err!=nil{return err}
 before,params,err:=q.Reader.ReadExportSeriesMeasureParams(ctx,balance,entry,append([]byte(nil),body...));if err!=nil{return err}
 bundle,err:=canonical.Marshal(struct{Schema int `json:"schema_version"`;Stats exportCompositionDocument `json:"stats"`;Params exportCompositionDocument `json:"params"`;Image exportCompositionDocument `json:"image"`}{1,exportCompositionDocument{SHA:entry.Ref.SHA256,Body:body},exportCompositionDocument{SHA:before.Params.SHA256,Body:params},exportCompositionDocument{SHA:image.Entry.Ref.SHA256,Body:imageBody}});if err!=nil{return err}
 if _,err:=q.native.ExportDocumentChildren(ctx,v2input.Entry{Contract:"SeriesMeasureOrigin",Ref:v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:rawSHA(bundle)}},bundle);err!=nil{return err}
 if err:=q.Inputs.QualifyExportSeriesMeasureInputs(ctx,before,entry,append([]byte(nil),params...),image);err!=nil{return err}
 after,finalParams,err:=q.Reader.ReadExportSeriesMeasureParams(ctx,balance,entry,append([]byte(nil),body...));if err!=nil{return err}
 if after!=before||!bytes.Equal(params,finalParams){return v2input.ErrInvalid}
 if err:=q.Inputs.QualifyExportSeriesMeasureInputs(ctx,after,entry,append([]byte(nil),finalParams...),image);err!=nil{return err};return ctx.Err()
}
var _ ExportSeriesMeasureAuthority=(*NativeExportSeriesMeasureQualifier)(nil)
