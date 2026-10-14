package interchange

import (
 "bytes"
 "context"
 "photoeditor/backend/internal/v2input"
)

type ExportBodyFitBundleReader interface {
 ReadExportBodyFitInputs(context.Context,v2input.Entry,[]byte,ExportClosureNode)([]byte,error)
}
// Must qualify both source sides, alignment provenance, current feature model
// admission/consent, and physical person masks. Native schemas alone are insufficient.
type ExportBodyFitCausalAuthority interface {
 QualifyExportBodyFitCausalInputs(context.Context,v2input.Entry,[]byte,ExportClosureNode,[]byte)error
}
type NativeExportBodyFitQualifier struct {
 Reader ExportBodyFitBundleReader
 Causal ExportBodyFitCausalAuthority
 native *NativeExportStyleValidator
}
func NewNativeExportBodyFitQualifier(reader ExportBodyFitBundleReader,causal ExportBodyFitCausalAuthority,scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string)(*NativeExportBodyFitQualifier,error){
 if reader==nil||causal==nil{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,exportCompositionStagingValidator{});if err!=nil{return nil,err}
 native.contract="BodyFitOrigin"
 return &NativeExportBodyFitQualifier{Reader:reader,Causal:causal,native:native},nil
}
func(q *NativeExportBodyFitQualifier)QualifyExportBodyFitInputs(ctx context.Context,parent v2input.Entry,raw []byte,fit ExportClosureNode)error{
 if err:=ctx.Err();err!=nil{return err}
 if q==nil||q.Reader==nil||q.Causal==nil||q.native==nil||parent.Contract!="Params"||!validImportRef(parent.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=parent.Ref.SHA256{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 owned:=ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}
 if _,err:=exportBodyFitInputInventory(owned);err!=nil{return err}
 clone:=func()ExportClosureNode{return ExportClosureNode{Entry:owned.Entry,Body:append([]byte(nil),owned.Body...)}}
 bundle,err:=q.Reader.ReadExportBodyFitInputs(ctx,parent,append([]byte(nil),body...),clone());if err!=nil{return err}
 bundle=append([]byte(nil),bundle...)
 if len(bundle)<2||len(bundle)>16<<20{return v2input.ErrInvalid}
 if err:=q.Causal.QualifyExportBodyFitCausalInputs(ctx,parent,append([]byte(nil),body...),clone(),append([]byte(nil),bundle...));err!=nil{return err}
 synthetic:=v2input.Entry{Contract:"BodyFitOrigin",Ref:v2input.Ref{ArtifactID:owned.Entry.Ref.ArtifactID,SHA256:rawSHA(bundle)}}
 if _,err:=q.native.ExportDocumentChildren(ctx,synthetic,bundle);err!=nil{return err}
 final,err:=q.Reader.ReadExportBodyFitInputs(ctx,parent,append([]byte(nil),body...),clone());if err!=nil{return err}
 if !bytes.Equal(bundle,final){return v2input.ErrInvalid}
 if err:=q.Causal.QualifyExportBodyFitCausalInputs(ctx,parent,append([]byte(nil),body...),clone(),append([]byte(nil),final...));err!=nil{return err}
 return ctx.Err()
}
var _ ExportBodyFitInputAuthority=(*NativeExportBodyFitQualifier)(nil)
