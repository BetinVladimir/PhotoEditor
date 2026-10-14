package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// Source must authorize and physically verify the immutable snapshot, and bind
// its frame resolver to this tenant/project/actor/request. The compiler cannot
// infer portable recipe or encoded-image lineage from arbitrary artifact refs.
type ExportProposalInputs interface{
 ResolveExportProposalInputs(context.Context,auth.Identity,uuid.UUID,CreateRequest,string)(v2input.Ref,[]byte,ExportFrameResolver,error)
}
type ExportProposalSource struct{Inputs ExportProposalInputs;Limits ExportLimits}
func(s ExportProposalSource)ProposeExportDocuments(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(FrozenExportDocuments,error){
 var zero FrozenExportDocuments
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Inputs==nil||s.Limits.ArchiveBytes<1||s.Limits.ArchiveBytes>64<<30||s.Limits.ExpandedBytes<1||s.Limits.ExpandedBytes>128<<30||s.Limits.Files<1||s.Limits.Files>30000{return zero,v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return zero,err}
 if intent.Request.Direction!="export"{return zero,v2input.ErrInvalid}
 ref,body,frames,err:=s.Inputs.ResolveExportProposalInputs(ctx,who,tid,copyExportIntent(intent).Request,key);if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 raw:=append([]byte(nil),body...)
 if frames==nil||!validImportRef(ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=ref.SHA256{return zero,v2input.ErrInvalid}
 snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(ref,raw);if err!=nil{return zero,err}
 if snapshot.SnapshotID!=intent.Identity.Snapshot{return zero,v2input.ErrInvalid}
 compiled,err:=CompileExportPlan(ctx,intent.Identity.Workflow,intent.Request.ProjectID,intent.Request.Format,ref,raw,s.Limits,frames);if err!=nil{return zero,err}
 return FreezeExportDocuments(ctx,intent,compiled,ref,raw)
}
var _ ExportFrozenProposalSource=ExportProposalSource{}
