package interchange

import (
 "bytes"
 "context"
 "strings"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Installed measurements are deployment evidence, never an HTTP parameter.
// Exact workload matching deliberately does not extrapolate native memory use.
type ExportResourceMeasurement struct {
 FrozenSHA string `json:"frozen_sha256"`
 InputDigest string `json:"input_digest"`
 RAM int64 `json:"ram_bytes"`
 Scratch int64 `json:"scratch_bytes"`
 Output int64 `json:"output_bytes"`
 InputJSON int64 `json:"max_input_json_bytes"`
}
type ExportResourceMeasurements struct {
 Schema string `json:"schema_version"`
 Runtime string `json:"runtime_sha256"`
 Contracts string `json:"contracts_sha256"`
 Hardware string `json:"hardware_sha256"`
 Measurements []ExportResourceMeasurement `json:"measurements"`
}
type MeasuredExportResourceQualifier struct {
 contracts string
 measurements map[string]ExportTaskResources
}
func NewMeasuredExportResourceQualifier(raw []byte,pin,runtime,contracts,hardware string)(*MeasuredExportResourceQualifier,error){
 if len(raw)<2||len(raw)>8<<20||!validImportRefForSHA(pin)||rawSHA(raw)!=pin||!validImportRefForSHA(runtime)||!validImportRefForSHA(contracts)||!validImportRefForSHA(hardware){return nil,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(raw,normalized){return nil,v2input.ErrInvalid}
 var report ExportResourceMeasurements
 if err:=decodeImportDocument(raw,&report);err!=nil{return nil,err}
 if report.Schema!="interchange-export-resource-measurements/v1"||report.Runtime!=runtime||report.Contracts!=contracts||report.Hardware!=hardware||len(report.Measurements)<1||len(report.Measurements)>4096{return nil,v2input.ErrInvalid}
 q:=&MeasuredExportResourceQualifier{contracts:contracts,measurements:map[string]ExportTaskResources{}}
 const safe int64=9007199254740991
 for _,m:=range report.Measurements{
  if !validImportRefForSHA(m.FrozenSHA)||!validImportRefForSHA(m.InputDigest)||m.InputJSON<1||m.InputJSON>128<<20||m.RAM<m.InputJSON||m.RAM>safe||m.Scratch<1||m.Scratch>safe||m.Output<1||m.Output>safe{return nil,v2input.ErrInvalid}
  key:=m.FrozenSHA+":"+m.InputDigest
  if _,exists:=q.measurements[key];exists{return nil,v2input.ErrInvalid}
  q.measurements[key]=ExportTaskResources{RAMBytes:m.RAM,ScratchBytes:m.Scratch,OutputBytes:m.Output,MaxInputJSONBytes:m.InputJSON,ContractsSHA:contracts}
 }
 return q,nil
}
func(q *MeasuredExportResourceQualifier)QualifyExportResources(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,inventory ExportInputInventory)(ExportTaskResources,error){
 var zero ExportTaskResources
 if err:=ctx.Err();err!=nil{return zero,err}
 if q==nil||!validImportRefForSHA(q.contracts){return zero,v2input.ErrInvalid}
 resources,exists:=q.measurements[rawSHA(frozen.Body())+":"+inventory.digest]
 if !exists{return zero,v2input.ErrInvalid}
 // Revalidate task, inventory contents, complete frozen identity and protocol
 // floors; a valid measurement key alone is not proof of a valid task graph.
 if _,err:=CompileExportTask(ctx,intent,frozen,inventory,resources);err!=nil{return zero,err}
 return resources,nil
}
var _ ExportResourceQualifier=(*MeasuredExportResourceQualifier)(nil)

// Selection reserves a candidate envelope before the inventory can be read.
// It is not qualification: exact inventory matching remains mandatory before
// materialization and again before activation. Ambiguous envelopes are refused.
func(q *MeasuredExportResourceQualifier)SelectExportResources(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments)(ExportTaskResources,error){
 var zero ExportTaskResources
 if err:=ctx.Err();err!=nil{return zero,err}
 if q==nil{return zero,v2input.ErrInvalid}
 if _,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());err!=nil{return zero,err}
 prefix:=rawSHA(frozen.Body())+":"
 found:=false;var selected ExportTaskResources
 for key,resources:=range q.measurements{
  if err:=ctx.Err();err!=nil{return zero,err}
  if !strings.HasPrefix(key,prefix){continue}
  if found&&resources!=selected{return zero,v2input.ErrInvalid}
  selected=resources;found=true
 }
 if !found{return zero,v2input.ErrInvalid}
 return selected,nil
}
