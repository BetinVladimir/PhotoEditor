package interchange
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
// Complete native module validation supplements mandatory current policy; no
// archived approval, schema receipt or local UUID grants execution authority.
type NativeImportOverrideContent struct{native *NativeExportStyleValidator;policy ImportOverrideContentQualifier}
type importNativeOverrideAuthority struct{policy ImportOverrideContentQualifier;binding v2input.SessionVersionBinding}
func(a importNativeOverrideAuthority)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if a.policy==nil||entry.Contract!="Override"||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 return nil,a.policy.QualifyImportOverrideContent(ctx,append([]byte(nil),raw...),a.binding)
}
func NewNativeImportOverrideContent(scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime string,policy ImportOverrideContentQualifier)(*NativeImportOverrideContent,error){
 if policy==nil{return nil,v2input.ErrInvalid}
 native,err:=NewNativeExportOverrideValidator(scratch,probe,models,runtime,importNativeOverrideAuthority{policy:policy});if err!=nil{return nil,err}
 return &NativeImportOverrideContent{native:native,policy:policy},nil
}
func(q *NativeImportOverrideContent)QualifyImportOverrideContent(ctx context.Context,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if q==nil||q.native==nil||q.policy==nil||binding.AssetVersionID==uuid.Nil||!validImportRef(binding.Source)||!validImportRefForSHA(binding.CanvasHash)||len(raw)<2||len(raw)>8<<20{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...);sha:=rawSHA(body)
 entry:=v2input.Entry{Contract:"Override",Ref:v2input.Ref{ArtifactID:uuid.NewSHA1(uuid.NameSpaceOID,[]byte("interchange:validate-override:"+sha)),SHA256:sha}}
 authority:=importNativeOverrideAuthority{policy:q.policy,binding:binding}
 if _,err:=authority.ExportDocumentChildren(ctx,entry,body);err!=nil{return err}
 native:=*q.native;native.authority=authority
 _,err:=native.ExportDocumentChildren(ctx,entry,body);return err
}
var _ ImportOverrideContentQualifier=(*NativeImportOverrideContent)(nil)
