package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
// JSONB ledger formatting is normalized; embedded document strings retain
// exact canonical bytes and are independently recompiled before recovery.
func DecodeFrozenExportDocuments(ctx context.Context,intent CreateIntent,raw []byte)(FrozenExportDocuments,CompiledExportPlan,error){
 var frozen FrozenExportDocuments;var compiled CompiledExportPlan
 if err:=ctx.Err();err!=nil{return frozen,compiled,err}
 if len(raw)<2||len(raw)>24<<20{return frozen,compiled,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil{return frozen,compiled,v2input.ErrInvalid}
 var payload exportFreezePayload
 decoder:=json.NewDecoder(bytes.NewReader(normalized));decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&payload);err!=nil{return frozen,compiled,err}
 encoded,err:=canonical.Marshal(payload);if err!=nil||!bytes.Equal(encoded,normalized)||payload.Schema!=1||(payload.QueueClass!=""&&payload.QueueClass!="batch")||payload.Identity!=intent.Identity||payload.RequestSHA!=intent.RequestSHA{return frozen,compiled,v2input.ErrInvalid}
 request,err:=canonical.Marshal(payload.Request);if err!=nil||!bytes.Equal(request,intent.RequestJCS){return frozen,compiled,v2input.ErrInvalid}
 var plan importPlanDocument;if err:=decodeImportDocument([]byte(payload.PlanJCS),&plan);err!=nil{return frozen,compiled,err}
 frames:=map[uuid.UUID]ExportFrame{}
 for _,entry:=range plan.Entries{
  if entry.Binding==nil||entry.Recipe==nil{return frozen,compiled,v2input.ErrInvalid}
  if _,ok:=frames[entry.Binding.AssetVersionID];ok{return frozen,compiled,v2input.ErrInvalid}
  frames[entry.Binding.AssetVersionID]=ExportFrame{ExternalID:entry.External,Path:entry.Path,Content:entry.Content,PortableRecipe:*entry.Recipe,Rating:entry.Rating,Pick:entry.Pick,Crop:entry.Crop}
 }
 compiled,err=CompileExportPlan(ctx,intent.Identity.Workflow,intent.Request.ProjectID,intent.Request.Format,payload.Snapshot,[]byte(payload.SnapshotJCS),ExportLimits{plan.Archive,plan.Expanded,plan.Files},exportRecoveryFrames(frames))
 if err!=nil{return frozen,CompiledExportPlan{},err}
 pref,pbody:=compiled.PlanDocument();paramsRef,paramsBody:=compiled.ParamsDocument()
 if pref!=payload.Plan||paramsRef!=payload.Params||string(pbody)!=payload.PlanJCS||string(paramsBody)!=payload.ParamsJCS{return frozen,CompiledExportPlan{},v2input.ErrInvalid}
 frozen,err=freezeExportDocumentsForQueue(ctx,intent,compiled,payload.Snapshot,[]byte(payload.SnapshotJCS),payload.QueueClass)
 if err!=nil||!bytes.Equal(frozen.body,normalized){return FrozenExportDocuments{},CompiledExportPlan{},v2input.ErrInvalid}
 return frozen,compiled,ctx.Err()
}
type exportRecoveryFrames map[uuid.UUID]ExportFrame
func(f exportRecoveryFrames)ResolveExportFrame(ctx context.Context,item v2input.SessionSnapshotItem)(ExportFrame,error){if err:=ctx.Err();err!=nil{return ExportFrame{},err};frame,ok:=f[item.Binding.AssetVersionID];if !ok{return ExportFrame{},v2input.ErrInvalid};return frame,nil}
