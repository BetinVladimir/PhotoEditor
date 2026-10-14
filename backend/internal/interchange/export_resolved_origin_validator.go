package interchange
import (
 "context"
 "encoding/json"
 "io"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type ExportResolvedOriginSource interface {LookupExportResolvedOrigin(context.Context,v2input.Entry,[]byte)(ExportResolvedOriginEvidence,error)}
type ExportResolvedOriginValidator struct {
 Other ExportTypedDocumentValidator
 Origin ExportResolvedOriginSource
 Objects ImportDocumentObjects
 native *NativeExportStyleValidator
}
func NewExportResolvedOriginValidator(other ExportTypedDocumentValidator,origin ExportResolvedOriginSource,objects ImportDocumentObjects,scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string)(*ExportResolvedOriginValidator,error){
 if other==nil||origin==nil||objects==nil{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,exportCompositionStagingValidator{});if err!=nil{return nil,err};native.contract="ResolvedOrigin"
 return &ExportResolvedOriginValidator{Other:other,Origin:origin,Objects:objects,native:native},nil
}
func(v *ExportResolvedOriginValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if v==nil||v.Other==nil||v.Origin==nil||v.Objects==nil||v.native==nil{return nil,v2input.ErrInvalid}
 if entry.Contract=="ResolvedStyle"&&(!validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256){return nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 children,err:=v.Other.ExportDocumentChildren(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err}
 if entry.Contract!="ResolvedStyle"{return children,ctx.Err()}
 if !validImportRef(entry.Ref)||len(body)<2||len(body)>8<<20||rawSHA(body)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 evidence,err:=v.Origin.LookupExportResolvedOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err}
 object:=evidence.Object
 if !validImportRef(evidence.Params)||object.SHA256!=evidence.Params.SHA256||object.ContentType!="application/json"||object.Bytes<2||object.Bytes>1<<20||object.Key==""||object.VersionID==""||object.VersionID=="null"{return nil,v2input.ErrInvalid}
 stream,err:=v.Objects.Open(ctx,object);if err!=nil{if stream!=nil{_ = stream.Close()};return nil,err};if stream==nil{return nil,v2input.ErrInvalid}
 params,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx,stream},object.Bytes+1));closeErr:=stream.Close()
 if readErr!=nil{return nil,readErr};if closeErr!=nil{return nil,closeErr}
 if int64(len(params))!=object.Bytes||rawSHA(params)!=evidence.Params.SHA256{return nil,v2input.ErrInvalid}
 bundle,err:=canonical.Marshal(struct{Schema int `json:"schema_version"`;Resolved exportCompositionDocument `json:"resolved"`;Params exportCompositionDocument `json:"params"`}{1,exportCompositionDocument{SHA:entry.Ref.SHA256,Body:json.RawMessage(body)},exportCompositionDocument{SHA:evidence.Params.SHA256,Body:json.RawMessage(params)}});if err!=nil{return nil,err}
 bundleEntry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:rawSHA(bundle)},Contract:"ResolvedOrigin"}
 if _,err:=v.native.ExportDocumentChildren(ctx,bundleEntry,bundle);err!=nil{return nil,err}
 current,err:=v.Origin.LookupExportResolvedOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err};if current!=evidence{return nil,v2input.ErrInvalid}
 if err:=validateExportResolvedChildren(entry,body,children);err!=nil{return nil,err}
 // Re-run current scoped source/model/consent authority after probe IO.
 // Every child must still have the same exact role and immutable reference.
 finalChildren,err:=v.Other.ExportDocumentChildren(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err}
 if err:=validateExportResolvedChildren(entry,body,finalChildren);err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err};return append([]v2input.Entry(nil),children...),nil
}
var _ ExportResolvedOriginSource=SQLExportDocumentCatalog{}
var _ ExportTypedDocumentValidator=(*ExportResolvedOriginValidator)(nil)
