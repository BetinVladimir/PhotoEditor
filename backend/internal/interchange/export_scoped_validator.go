package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

type ExportScopedDocumentValidator struct{
 plan ExportPlanDocumentValidator
 planBody,paramsBody,snapshotBody []byte
 params,snapshot v2input.Ref
 dependencies ExportTypedDocumentValidator
}
// Dependencies must install complete schema/source/runtime semantics for every
// non-root JSON contract. Missing validators reject rather than infer roles.
func NewExportScopedDocumentValidator(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,dependencies ExportTypedDocumentValidator)(ExportScopedDocumentValidator,error){
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return ExportScopedDocumentValidator{},err}
 var payload exportFreezePayload;if err:=json.Unmarshal(frozen.Body(),&payload);err!=nil{return ExportScopedDocumentValidator{},err}
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument([]byte(payload.SnapshotJCS),&snapshot);err!=nil{return ExportScopedDocumentValidator{},err}
 return ExportScopedDocumentValidator{plan:ExportPlanDocumentValidator{Task:compiled.task,Project:intent.Request.ProjectID,SnapshotDigest:snapshot.SnapshotDigest,Plan:compiled.planRef},planBody:append([]byte(nil),compiled.planBody...),paramsBody:append([]byte(nil),compiled.paramsBody...),snapshotBody:[]byte(payload.SnapshotJCS),params:compiled.paramsRef,snapshot:payload.Snapshot,dependencies:dependencies},nil
}
func(v ExportScopedDocumentValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if v.plan.Task==uuid.Nil||!validImportRef(entry.Ref)||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 switch entry.Contract{
 case "Params":
  if entry.Ref==v.params{if !bytes.Equal(raw,v.paramsBody){return nil,v2input.ErrInvalid};return v.plan.ExportDocumentChildren(ctx,entry,raw)}
 case "InterchangePlan":
  if entry.Ref!=v.plan.Plan||!bytes.Equal(raw,v.planBody){return nil,v2input.ErrInvalid};return v.plan.ExportDocumentChildren(ctx,entry,raw)
 case "InputSnapshot":
  if entry.Ref!=v.snapshot||!bytes.Equal(raw,v.snapshotBody){return nil,v2input.ErrInvalid}
  var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument(raw,&snapshot);err!=nil{return nil,err}
  _,checked,err:=v2input.CompilePinnedSessionInputSnapshot(snapshot.SnapshotID,snapshot.Items)
  if err!=nil||!bytes.Equal(raw,checked)||snapshot.SnapshotDigest!=v.plan.SnapshotDigest{return nil,v2input.ErrInvalid}
  children:=[]v2input.Entry{};seen:=map[uuid.UUID]v2input.Entry{}
  for _,item:=range snapshot.Items{for _,child:=range []v2input.Entry{{Ref:item.WorkingImage,Contract:"WorkingImage"},{Ref:item.Recipe,Contract:"FrozenRenderRecipe"}}{
   if old,exists:=seen[child.Ref.ArtifactID];exists{if old!=child{return nil,v2input.ErrInvalid};continue}
   seen[child.Ref.ArtifactID]=child;children=append(children,child)
  }}
  return children,ctx.Err()
 }
 if exportBinaryContract(entry.Contract)||v.dependencies==nil{return nil,v2input.ErrInvalid}
 return v.dependencies.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...))
}
