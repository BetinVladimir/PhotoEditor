package interchange

import (
 "bytes"
 "context"
 "photoeditor/backend/internal/v2input"
)

// Must qualify current pose/landmark model admission, source/fit provenance and
// consent. Native content checks and catalogue readability do not grant this.
type ExportBodyOriginPolicy interface{
 QualifyExportBodyOrigin(context.Context,ExportParamsOperationEvidence,v2input.Entry,[]byte,[]byte)error
}
type NativeExportBodyOriginValidator struct{
 Reader ExportBodyOriginReader
 Policy ExportBodyOriginPolicy
 Other ExportTypedDocumentValidator
 native *NativeExportStyleValidator
}
func NewNativeExportBodyOriginValidator(reader ExportBodyOriginReader,policy ExportBodyOriginPolicy,other ExportTypedDocumentValidator,scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string)(*NativeExportBodyOriginValidator,error){
 if reader.Source==nil||reader.Documents==nil||policy==nil||other==nil{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,exportCompositionStagingValidator{});if err!=nil{return nil,err};native.contract="BodyOrigin"
 return &NativeExportBodyOriginValidator{Reader:reader,Policy:policy,Other:other,native:native},nil
}
func(v *NativeExportBodyOriginValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if v==nil||v.native==nil||v.Policy==nil||v.Other==nil||v.Reader.Source==nil||v.Reader.Documents==nil{return nil,v2input.ErrInvalid}
 if entry.Contract!="Params"{return v.Other.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...))}
 if len(raw)<2||len(raw)>1<<20||!validImportRef(entry.Ref)||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 operation,err:=exportParamsOperationShape(body);if err!=nil{return nil,err}
 if operation!="geometry.body"{return v.Other.ExportDocumentChildren(ctx,entry,body)}
 before,bundle,err:=v.Reader.ReadExportBodyOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err}
 if err:=v.Policy.QualifyExportBodyOrigin(ctx,before,entry,append([]byte(nil),body...),append([]byte(nil),bundle...));err!=nil{return nil,err}
 synthetic:=v2input.Entry{Contract:"BodyOrigin",Ref:v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:rawSHA(bundle)}}
 if _,err:=v.native.ExportDocumentChildren(ctx,synthetic,bundle);err!=nil{return nil,err}
 children,err:=v.Other.ExportDocumentChildren(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err}
 children=append([]v2input.Entry(nil),children...)
 after,finalBundle,err:=v.Reader.ReadExportBodyOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err}
 if before!=after||!bytes.Equal(bundle,finalBundle){return nil,v2input.ErrInvalid}
 if err:=v.Policy.QualifyExportBodyOrigin(ctx,after,entry,append([]byte(nil),body...),append([]byte(nil),finalBundle...));err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err};return children,nil
}
var _ ExportTypedDocumentValidator=(*NativeExportBodyOriginValidator)(nil)
