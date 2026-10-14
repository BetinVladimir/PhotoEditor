package interchange

import (
 "bytes"
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Implementations must use current operator qualification and stored producer
// execution locks. Original Params/result schemas alone do not satisfy this.
type ExportBodyDependencyProfileAuthority interface{
 QualifyExportBodyDependencyProfile(context.Context,ExportBodyDependencyOrigin,v2input.Entry,[]byte,v2input.Entry)error
}
type NativeExportBodyDependencyQualifier struct{Reader ExportBodyDependencyParamsReader;Profile ExportBodyDependencyProfileAuthority;native *NativeExportStyleValidator}
func NewNativeExportBodyDependencyQualifier(reader ExportBodyDependencyParamsReader,profile ExportBodyDependencyProfileAuthority,scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string)(*NativeExportBodyDependencyQualifier,error){
 if reader.Source==nil||reader.Objects==nil||profile==nil{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,exportCompositionStagingValidator{});if err!=nil{return nil,err};native.contract="BodyDependencyOrigin"
 return &NativeExportBodyDependencyQualifier{Reader:reader,Profile:profile,native:native},nil
}
func(q *NativeExportBodyDependencyQualifier)QualifyExportBodyDependency(ctx context.Context,parent v2input.Entry,raw []byte,child ExportClosureNode)error{
 if err:=ctx.Err();err!=nil{return err}
 if q==nil||q.native==nil||q.Profile==nil||q.Reader.Source==nil||q.Reader.Objects==nil{return v2input.ErrInvalid}
 if len(raw)<2||len(raw)>1<<20{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 result,err:=ownExportCompositionDocument(ctx,child,child.Entry);if err!=nil{return err};if len(result.Body)>2<<20{return v2input.ErrInvalid}
 before,params,err:=q.Reader.ReadExportBodyDependencyParams(ctx,parent,append([]byte(nil),body...),child.Entry);if err!=nil{return err}
 if before.Output.Bytes!=int64(len(result.Body)){return v2input.ErrInvalid}
 if err:=q.Profile.QualifyExportBodyDependencyProfile(ctx,before,parent,append([]byte(nil),body...),child.Entry);err!=nil{return err}
 bundle,err:=canonical.Marshal(struct{
  Schema int `json:"schema_version"`;Operation string `json:"operation"`;Version string `json:"version"`
  Params exportCompositionDocument `json:"params"`;Result exportCompositionDocument `json:"result"`
 }{1,before.Producer.Operation,before.Producer.Version,exportCompositionDocument{SHA:before.Params.SHA256,Body:params},result});if err!=nil{return err}
 synthetic:=v2input.Entry{Contract:"BodyDependencyOrigin",Ref:v2input.Ref{ArtifactID:child.Entry.Ref.ArtifactID,SHA256:rawSHA(bundle)}}
 if _,err:=q.native.ExportDocumentChildren(ctx,synthetic,bundle);err!=nil{return err}
 after,finalParams,err:=q.Reader.ReadExportBodyDependencyParams(ctx,parent,append([]byte(nil),body...),child.Entry);if err!=nil{return err}
 if before!=after||!bytes.Equal(params,finalParams){return v2input.ErrInvalid}
 if err:=q.Profile.QualifyExportBodyDependencyProfile(ctx,after,parent,append([]byte(nil),body...),child.Entry);err!=nil{return err};return ctx.Err()
}
