package interchange

import (
 "context"
 "bytes"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// ExportTaskResources is supplied by the trusted preparation/qualification layer,
// never copied from an HTTP payload. Floors below cover protocol bookkeeping;
// native decoding, recipes and masks require independent resource qualification.
type ExportTaskResources struct{
 RAMBytes int64
 ScratchBytes int64
 OutputBytes int64
 MaxInputJSONBytes int64
 ContractsSHA string
}

// CompileExportTask builds a blocked-shell specification, not an activated task.
// SQL admission must independently verify these estimates before materialization.
func CompileExportTask(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,inventory ExportInputInventory,resources ExportTaskResources)(v2input.DraftTask,error){
 var zero v2input.DraftTask
 if err:=ctx.Err();err!=nil{return zero,err}
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return zero,err}
 if inventory.task!=compiled.task||len(inventory.entries)==0||len(inventory.entries)>v2input.MaxEntries{return zero,v2input.ErrInvalid}
 digest,err:=canonical.Hash(inventory.entries);if err!=nil||digest!=inventory.digest{return zero,v2input.ErrInvalid}
 seen:=map[uuid.UUID]v2input.Entry{}
 for index,entry:=range inventory.entries{
  if index>0&&bytes.Compare(inventory.entries[index-1].Ref.ArtifactID[:],entry.Ref.ArtifactID[:])>=0{return zero,v2input.ErrInvalid}
  if !validImportRef(entry.Ref)||entry.Contract==""{return zero,v2input.ErrInvalid}
  if _,exists:=seen[entry.Ref.ArtifactID];exists{return zero,v2input.ErrInvalid};seen[entry.Ref.ArtifactID]=entry
 }
 for _,root:=range compiled.roots{if seen[root.Ref.ArtifactID]!=root{return zero,v2input.ErrInvalid}}
 var plan importPlanDocument;if err:=decodeImportDocument(compiled.planBody,&plan);err!=nil{return zero,err}
 if !validImportRef(v2input.Ref{ArtifactID:compiled.task,SHA256:resources.ContractsSHA}){return zero,v2input.ErrInvalid}
 const safe int64=9007199254740991
 if resources.MaxInputJSONBytes<1||resources.MaxInputJSONBytes>128<<20||resources.RAMBytes<resources.MaxInputJSONBytes||resources.RAMBytes>safe ||
 resources.OutputBytes<plan.Archive+(16<<20)||resources.OutputBytes>safe ||
 resources.ScratchBytes<plan.Archive||resources.ScratchBytes>safe{return zero,v2input.ErrInvalid}
 profile,err:=canonical.Marshal(struct{
  Backend string `json:"backend"`;Renderer string `json:"renderer_version"`;ContractsSHA string `json:"contracts_sha"`;MaxInputJSON int64 `json:"max_input_json_bytes"`
  Estimate struct{RAM int64 `json:"ram_bytes"`;VRAM int64 `json:"vram_bytes"`;Scratch int64 `json:"scratch_bytes"`;Output int64 `json:"output_bytes"`;Pixels int64 `json:"pixel_count"`} `json:"resource_estimate"`
 }{Backend:"cpu",Renderer:"v2.2",ContractsSHA:resources.ContractsSHA,MaxInputJSON:resources.MaxInputJSONBytes,Estimate:struct{RAM int64 `json:"ram_bytes"`;VRAM int64 `json:"vram_bytes"`;Scratch int64 `json:"scratch_bytes"`;Output int64 `json:"output_bytes"`;Pixels int64 `json:"pixel_count"`}{RAM:resources.RAMBytes,Scratch:resources.ScratchBytes,Output:resources.OutputBytes}})
 if err!=nil{return zero,err}
 return v2input.DraftTask{ID:compiled.task,NodeKey:"v2.interchange.export:0",Operation:"v2.interchange.export",OperationVersion:"1",ResourceClass:"cpu",ParamsArtifactID:compiled.paramsRef.ArtifactID,ParamsSHA:compiled.paramsRef.SHA256,InputDigest:digest,ExecutionProfile:json.RawMessage(profile)},nil
}
