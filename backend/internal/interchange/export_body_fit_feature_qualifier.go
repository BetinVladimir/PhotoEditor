package interchange

import (
 "bytes"
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Implementations must use current operator qualification and stored producer
// execution locks. Original Params/result schemas alone do not satisfy this.
type ExportBodyFitFeatureProfileAuthority interface{
 QualifyExportBodyFitFeatureProfile(context.Context,ExportBodyDependencyOrigin,v2input.Entry,[]byte,ExportClosureNode,v2input.Entry,[]byte)error
}
type NativeExportBodyFitFeatureQualifier struct{Reader ExportBodyFitFeatureParamsReader;Profile ExportBodyFitFeatureProfileAuthority;native *NativeExportStyleValidator}
func NewNativeExportBodyFitFeatureQualifier(reader ExportBodyFitFeatureParamsReader,profile ExportBodyFitFeatureProfileAuthority,scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string)(*NativeExportBodyFitFeatureQualifier,error){
 if reader.Source==nil||reader.Objects==nil||profile==nil{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,exportCompositionStagingValidator{});if err!=nil{return nil,err};native.contract="BodyDependencyOrigin"
 return &NativeExportBodyFitFeatureQualifier{Reader:reader,Profile:profile,native:native},nil
}
func(q *NativeExportBodyFitFeatureQualifier)QualifyExportBodyFitFeature(ctx context.Context,parent v2input.Entry,raw []byte,fit,child ExportClosureNode)error{
 if err:=ctx.Err();err!=nil{return err}
 if q==nil||q.native==nil||q.Profile==nil||q.Reader.Source==nil||q.Reader.Objects==nil{return v2input.ErrInvalid}
 if len(raw)<2||len(raw)>1<<20{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 fit=ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}
 clone:=func()ExportClosureNode{return ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}}
 result,err:=ownExportCompositionDocument(ctx,child,child.Entry);if err!=nil{return err};if len(result.Body)>2<<20{return v2input.ErrInvalid}
 before,params,err:=q.Reader.ReadExportBodyFitFeatureParams(ctx,parent,append([]byte(nil),body...),clone(),child.Entry);if err!=nil{return err}
 if before.Output.Bytes!=int64(len(result.Body)){return v2input.ErrInvalid}
 if err:=q.Profile.QualifyExportBodyFitFeatureProfile(ctx,before,parent,append([]byte(nil),body...),clone(),child.Entry,append([]byte(nil),params...));err!=nil{return err}
 bundle,err:=canonical.Marshal(struct{
  Schema int `json:"schema_version"`;Operation string `json:"operation"`;Version string `json:"version"`
  Params exportCompositionDocument `json:"params"`;Result exportCompositionDocument `json:"result"`
 }{1,before.Producer.Operation,before.Producer.Version,exportCompositionDocument{SHA:before.Params.SHA256,Body:params},result});if err!=nil{return err}
 synthetic:=v2input.Entry{Contract:"BodyDependencyOrigin",Ref:v2input.Ref{ArtifactID:child.Entry.Ref.ArtifactID,SHA256:rawSHA(bundle)}}
 if _,err:=q.native.ExportDocumentChildren(ctx,synthetic,bundle);err!=nil{return err}
 after,finalParams,err:=q.Reader.ReadExportBodyFitFeatureParams(ctx,parent,append([]byte(nil),body...),clone(),child.Entry);if err!=nil{return err}
 if before!=after||!bytes.Equal(params,finalParams){return v2input.ErrInvalid}
 if err:=q.Profile.QualifyExportBodyFitFeatureProfile(ctx,after,parent,append([]byte(nil),body...),clone(),child.Entry,append([]byte(nil),finalParams...));err!=nil{return err};return ctx.Err()
}
