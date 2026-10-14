package interchange

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type exportFreezePayload struct{
 QueueClass string `json:"queue_class,omitempty"`
 Schema int `json:"schema_version"`
 Request CreateRequest `json:"request"`
 RequestSHA string `json:"request_sha256"`
 Identity CreateIdentity `json:"identity"`
 Snapshot v2input.Ref `json:"snapshot"`
 SnapshotJCS string `json:"snapshot_jcs"`
 Plan v2input.Ref `json:"plan"`
 PlanJCS string `json:"plan_jcs"`
 Params v2input.Ref `json:"params"`
 ParamsJCS string `json:"params_jcs"`
}
type FrozenExportDocuments struct{body []byte;identity CreateIdentity}
func(f FrozenExportDocuments)Body()[]byte{return append([]byte(nil),f.body...)}
func(f FrozenExportDocuments)Identity()CreateIdentity{return f.identity}
// This immutable recovery payload belongs in a fenced server ledger. Merely
// constructing it does not authorize DB publication, task activation or IO.
func FreezeExportDocuments(ctx context.Context,intent CreateIntent,compiled CompiledExportPlan,snapshotRef v2input.Ref,snapshotRaw []byte)(FrozenExportDocuments,error){
 return freezeExportDocumentsForQueue(ctx,intent,compiled,snapshotRef,snapshotRaw,"batch")
}
func freezeExportDocumentsForQueue(ctx context.Context,intent CreateIntent,compiled CompiledExportPlan,snapshotRef v2input.Ref,snapshotRaw []byte,queue string)(FrozenExportDocuments,error){
 var zero FrozenExportDocuments
 if queue!=""&&queue!="batch"{return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 request:=intent.Request
 if request.validate()!=nil||request.Direction!="export"||request.SnapshotID==nil||compiled.task!=intent.Identity.Task||intent.Identity.Run==uuid.Nil||intent.Identity.Workflow==uuid.Nil||compiled.task!=uuid.NewSHA1(intent.Identity.Workflow,[]byte("v2.interchange.export:0"))||intent.Identity.Params!=compiled.paramsRef.ArtifactID{return zero,v2input.ErrInvalid}
 requestRaw,err:=canonical.Marshal(request);if err!=nil||!bytes.Equal(requestRaw,intent.RequestJCS)||rawSHA(requestRaw)!=intent.RequestSHA{return zero,v2input.ErrInvalid}
 raw:=append([]byte(nil),snapshotRaw...)
 if !validImportRef(snapshotRef)||rawSHA(raw)!=snapshotRef.SHA256{return zero,v2input.ErrInvalid}
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument(raw,&snapshot);err!=nil{return zero,err}
 _,checked,err:=v2input.CompilePinnedSessionInputSnapshot(snapshot.SnapshotID,snapshot.Items)
 if err!=nil||!bytes.Equal(checked,raw)||snapshot.SnapshotID!=*request.SnapshotID||intent.Identity.Snapshot!=snapshot.SnapshotID{return zero,v2input.ErrInvalid}
 validator:=ExportPlanDocumentValidator{Task:compiled.task,Project:request.ProjectID,SnapshotDigest:snapshot.SnapshotDigest,Plan:compiled.planRef}
 for _,doc:=range []struct{entry v2input.Entry;body []byte}{{v2input.Entry{Ref:compiled.planRef,Contract:"InterchangePlan"},compiled.planBody},{v2input.Entry{Ref:compiled.paramsRef,Contract:"Params"},compiled.paramsBody}}{if _,err:=validator.ExportDocumentChildren(ctx,doc.entry,doc.body);err!=nil{return zero,err}}
 var plan importPlanDocument;if err:=decodeImportDocument(compiled.planBody,&plan);err!=nil{return zero,err}
 if plan.Format!=request.Format||len(plan.Entries)!=len(snapshot.Items){return zero,v2input.ErrInvalid}
 versions:=map[uuid.UUID]v2input.SessionVersionBinding{};for _,item:=range snapshot.Items{versions[item.Binding.AssetVersionID]=item.Binding}
 for _,entry:=range plan.Entries{if entry.Binding==nil||versions[entry.Binding.AssetVersionID]!=*entry.Binding{return zero,v2input.ErrInvalid};delete(versions,entry.Binding.AssetVersionID)}
 if len(versions)!=0{return zero,v2input.ErrInvalid}
 found:=false;for _,root:=range compiled.roots{if root.Ref==snapshotRef&&root.Contract=="InputSnapshot"{found=true}}
 if !found{return zero,v2input.ErrInvalid}
 payload:=exportFreezePayload{QueueClass:queue,Schema:1,Request:request,RequestSHA:intent.RequestSHA,Identity:intent.Identity,Snapshot:snapshotRef,SnapshotJCS:string(raw),Plan:compiled.planRef,PlanJCS:string(compiled.planBody),Params:compiled.paramsRef,ParamsJCS:string(compiled.paramsBody)}
 body,err:=canonical.Marshal(payload);if err!=nil||len(body)>24<<20{return zero,v2input.ErrInvalid}
 return FrozenExportDocuments{body:body,identity:intent.Identity},ctx.Err()
}
