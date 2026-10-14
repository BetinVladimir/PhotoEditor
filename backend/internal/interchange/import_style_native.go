package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
// Implementations bind installed ICC/model/operator policies and current source
// authority. Archive qualification cannot inherit approval or consent claims.
type ImportStyleScopeAuthority interface{
 QualifyArchivedStyleScope(context.Context,[]byte)error
 QualifyTargetStyleScope(context.Context,[]byte,v2input.SessionVersionBinding)error
}
type NativeImportStyleQualifier struct{
 native *NativeExportStyleValidator
 scope ImportStyleScopeAuthority
}
type importNativeStyleAuthority struct{scope ImportStyleScopeAuthority;target *v2input.SessionVersionBinding}
func(a importNativeStyleAuthority)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if a.scope==nil||entry.Contract!="StyleV2"||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 var err error
 if a.target==nil{err=a.scope.QualifyArchivedStyleScope(ctx,body)}else{err=a.scope.QualifyTargetStyleScope(ctx,body,*a.target)}
 return nil,err
}
func NewNativeImportStyleQualifier(scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string,scope ImportStyleScopeAuthority)(*NativeImportStyleQualifier,error){
 if scope==nil{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportStyleValidator(scratch,probe,models,runtime,importNativeStyleAuthority{scope:scope});if err!=nil{return nil,err}
 return &NativeImportStyleQualifier{native:native,scope:scope},nil
}
func(q *NativeImportStyleQualifier)ValidateArchivedImportStyle(ctx context.Context,raw []byte)error{return q.qualify(ctx,raw,nil)}
func(q *NativeImportStyleQualifier)QualifyImportedStyle(ctx context.Context,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if binding.AssetVersionID==uuid.Nil||!validImportRef(binding.Source)||!validImportRef(v2input.Ref{ArtifactID:binding.AssetVersionID,SHA256:binding.CanvasHash}){return v2input.ErrInvalid}
 return q.qualify(ctx,raw,&binding)
}
func(q *NativeImportStyleQualifier)qualify(ctx context.Context,raw []byte,binding *v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if q==nil||q.native==nil||q.scope==nil||len(raw)<2||len(raw)>8<<20{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 authority:=importNativeStyleAuthority{scope:q.scope,target:binding}
 sha:=rawSHA(body)
 // The port validates document bytes, not an archive UUID. This local identity
 // is never published or used to grant artifact access.
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.NewSHA1(uuid.NameSpaceOID,[]byte("interchange:validate-style:"+sha)),SHA256:sha},Contract:"StyleV2"}
 if _,err:=authority.ExportDocumentChildren(ctx,entry,body);err!=nil{return err}
 native:=*q.native;native.authority=authority
 _,err:=native.ExportDocumentChildren(ctx,entry,body)
 return err
}
var _ ImportStyleQualifier=(*NativeImportStyleQualifier)(nil)
