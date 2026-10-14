package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
// Measurements are independently provisioned deployment evidence, not request
// estimates. Exact frozen-document and closure keys prohibit extrapolation.
type ImportResourceMeasurement struct {
 DocumentsSHA string `json:"documents_sha256"`
 InputDigest string `json:"input_digest"`
 RAM int64 `json:"ram_bytes"`
 Scratch int64 `json:"scratch_bytes"`
 Output int64 `json:"output_bytes"`
}
type ImportResourceMeasurements struct {
 Schema string `json:"schema_version"`
 Runtime string `json:"runtime_sha256"`
 Contracts string `json:"contracts_sha256"`
 Hardware string `json:"hardware_sha256"`
 Measurements []ImportResourceMeasurement `json:"measurements"`
}
type MeasuredImportResourceQualifier struct{measurements map[string]ImportResourceMeasurement}
func NewMeasuredImportResourceQualifier(raw []byte,pin,runtime,contracts,hardware string)(*MeasuredImportResourceQualifier,error){
 if len(raw)<2||len(raw)>8<<20||!validImportRefForSHA(pin)||rawSHA(raw)!=pin||!validImportRefForSHA(runtime)||!validImportRefForSHA(contracts)||!validImportRefForSHA(hardware){return nil,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return nil,v2input.ErrInvalid}
 var report ImportResourceMeasurements;if err:=decodeImportDocument(raw,&report);err!=nil{return nil,err}
 if report.Schema!="interchange-import-resource-measurements/v1"||report.Runtime!=runtime||report.Contracts!=contracts||report.Hardware!=hardware||len(report.Measurements)<1||len(report.Measurements)>4096{return nil,v2input.ErrInvalid}
 q:=&MeasuredImportResourceQualifier{measurements:map[string]ImportResourceMeasurement{}}
 const safe int64=9007199254740991
 for _,m:=range report.Measurements{
  if !validImportRefForSHA(m.DocumentsSHA)||!validImportRefForSHA(m.InputDigest)||m.RAM<1<<30||m.RAM>safe||m.Scratch<0||m.Scratch>safe||m.Output<1||m.Output>safe{return nil,v2input.ErrInvalid}
  key:=m.DocumentsSHA+":"+m.InputDigest;if _,exists:=q.measurements[key];exists{return nil,v2input.ErrInvalid};q.measurements[key]=m
 }
 return q,nil
}
func importResourceDocumentsSHA(documents ImportPreparedDocuments)(string,error){
 if documents.evidence==nil{return "",v2input.ErrInvalid}
 pins:=[]struct{Purpose string `json:"purpose"`;Ref v2input.Ref `json:"ref"` }{}
 for _,doc:=range documents.Documents(){if err:=validateImportStorageDocument(documents.identity,doc);err!=nil{return "",err};pins=append(pins,struct{Purpose string `json:"purpose"`;Ref v2input.Ref `json:"ref"`}{doc.Purpose,doc.Ref})}
 return canonical.Hash(pins)
}
func(q *MeasuredImportResourceQualifier)QualifyImportActivationResources(ctx context.Context,documents ImportPreparedDocuments,task v2input.DraftTask,inventory ImportInputInventory)error{
 if err:=ctx.Err();err!=nil{return err}
 if q==nil||inventory.tenant==([16]byte{})||inventory.documents.identity!=documents.identity{return v2input.ErrInvalid}
 if err:=validateImportTask(documents,task);err!=nil{return err}
 // Recompute from actual entries rather than trusting a supplied digest field.
 rebuilt,err:=FreezeImportInputs(inventory.tenant,documents,inventory.entries);if err!=nil{return err}
 if rebuilt.InputDigest()!=inventory.InputDigest()||rebuilt.InputDigest()!=task.InputDigest{return v2input.ErrInvalid}
 frozen,err:=importResourceDocumentsSHA(documents);if err!=nil{return err}
 m,ok:=q.measurements[frozen+":"+rebuilt.InputDigest()];if !ok{return v2input.ErrInvalid}
 var profile struct{Estimate struct{RAM int64 `json:"ram_bytes"`;VRAM int64 `json:"vram_bytes"`;Scratch int64 `json:"scratch_bytes"`;Output int64 `json:"output_bytes"`} `json:"resource_estimate"`}
 if json.Unmarshal(task.ExecutionProfile,&profile)!=nil||profile.Estimate.VRAM!=0||profile.Estimate.RAM!=m.RAM||profile.Estimate.Scratch!=m.Scratch||profile.Estimate.Output!=m.Output{return v2input.ErrInvalid}
 return ctx.Err()
}
var _ ImportActivationResourceAuthority=(*MeasuredImportResourceQualifier)(nil)


// BuildImportTask derives the complete execution profile from independently
// measured exact-workload evidence. It accepts no caller resource estimates.
// Inventory must have been physically resolved after root publication.
func(q *MeasuredImportResourceQualifier)BuildImportTask(ctx context.Context,documents ImportPreparedDocuments,inventory ImportInputInventory)(v2input.DraftTask,error){
 var zero v2input.DraftTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if q==nil||inventory.tenant==([16]byte{})||inventory.documents.identity!=documents.identity{return zero,v2input.ErrInvalid}
 documents=cloneImportPreparedDocuments(documents)
 rebuilt,err:=FreezeImportInputs(inventory.tenant,documents,append([]v2input.Entry(nil),inventory.entries...));if err!=nil{return zero,err}
 if rebuilt.InputDigest()!=inventory.InputDigest(){return zero,v2input.ErrInvalid}
 frozen,err:=importResourceDocumentsSHA(documents);if err!=nil{return zero,err}
 m,ok:=q.measurements[frozen+":"+rebuilt.InputDigest()];if !ok{return zero,v2input.ErrInvalid}
 profile,err:=canonical.Marshal(struct{
  Backend string `json:"backend"`
  Renderer string `json:"renderer_version"`
  Estimate struct{RAM int64 `json:"ram_bytes"`;VRAM int64 `json:"vram_bytes"`;Scratch int64 `json:"scratch_bytes"`;Output int64 `json:"output_bytes"`} `json:"resource_estimate"`
 }{Backend:"cpu",Renderer:"v2.2",Estimate:struct{RAM int64 `json:"ram_bytes"`;VRAM int64 `json:"vram_bytes"`;Scratch int64 `json:"scratch_bytes"`;Output int64 `json:"output_bytes"`}{RAM:m.RAM,VRAM:0,Scratch:m.Scratch,Output:m.Output}})
 if err!=nil{return zero,err}
 task:=v2input.DraftTask{ID:documents.TaskID(),NodeKey:"v2.interchange.import:0",Operation:"v2.interchange.import",OperationVersion:"1",ResourceClass:"cpu",ParamsArtifactID:documents.params.Ref.ArtifactID,ParamsSHA:documents.params.Ref.SHA256,InputDigest:rebuilt.InputDigest(),ExecutionProfile:profile}
 if err:=q.QualifyImportActivationResources(ctx,documents,task,rebuilt);err!=nil{return zero,err}
 return task,nil
}
