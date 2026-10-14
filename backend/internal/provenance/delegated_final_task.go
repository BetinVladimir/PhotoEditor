package provenance

import (
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// DecodeDelegatedFinalTaskEvidence accepts only SQL-projected final task data;
// it never treats this one node as proof of the complete execution graph.
func DecodeDelegatedFinalTaskEvidence(raw []byte,ready ReadySigningExport)(CanonicalSigningTask,error){
 if len(raw)<2||len(raw)>40<<20||ready.Task==uuid.Nil||ready.Run==uuid.Nil||
  ready.Ref.ArtifactID==uuid.Nil{return CanonicalSigningTask{},ErrInvalid}
 result,err:=decodeDelegatedTaskEvidence(raw,ready.Task,ready.Run)
 if err!=nil{return CanonicalSigningTask{},err}
 outputs,err:=canonicalTaskOutputRefs(result.Outputs)
 if err!=nil{return CanonicalSigningTask{},err}
 found:=false
 for _,ref:=range outputs{if ref==ready.Ref{found=true}}
 if !found{return CanonicalSigningTask{},ErrInvalid}
 return result,nil
}

// DecodeDelegatedSameRunTaskEvidence binds a SQL-projected ancestor to the
// requested task and final export run. It does not authorize cross-run links.
func DecodeDelegatedSameRunTaskEvidence(raw []byte,ready ReadySigningExport,
 requested uuid.UUID)(CanonicalSigningTask,error){
 if ready.Task==uuid.Nil||ready.Run==uuid.Nil||requested==uuid.Nil{
  return CanonicalSigningTask{},ErrInvalid
 }
 return decodeDelegatedTaskEvidence(raw,requested,ready.Run)
}

// DecodeDelegatedAncestorTaskEvidence permits a different run only because the
// scoped SQL ancestry predicate proves its accepted asset-version boundary.
func DecodeDelegatedAncestorTaskEvidence(raw []byte,ready ReadySigningExport,
 requested uuid.UUID)(CanonicalSigningTask,error){
 if ready.Task==uuid.Nil||ready.Run==uuid.Nil||requested==uuid.Nil{
  return CanonicalSigningTask{},ErrInvalid
 }
 return decodeDelegatedTaskEvidence(raw,requested,uuid.Nil)
}

func decodeDelegatedTaskEvidence(raw []byte,task,run uuid.UUID)(CanonicalSigningTask,error){
 var result CanonicalSigningTask
 if len(raw)<2||len(raw)>40<<20||task==uuid.Nil{
  return result,ErrInvalid
 }
 var wire struct {
  Task uuid.UUID `json:"task"`
  Run uuid.UUID `json:"run"`
  Result uuid.UUID `json:"result"`
  Attempt int `json:"attempt"`
  Fence int64 `json:"fence"`
  Operation string `json:"operation"`
  Version string `json:"version"`
  InputDigest string `json:"input_digest"`
  ResultDigest string `json:"result_digest"`
  Params v2input.Ref `json:"params"`
  Inputs json.RawMessage `json:"inputs"`
  Outputs json.RawMessage `json:"outputs"`
  Metrics json.RawMessage `json:"metrics"`
  ReviewRequired bool `json:"review_required"`
 }
 if json.Unmarshal(raw,&wire)!=nil||wire.Task!=task||
  (run!=uuid.Nil&&wire.Run!=run){
  return result,ErrInvalid
 }
 result=CanonicalSigningTask{Task:wire.Task,Run:wire.Run,Result:wire.Result,
  Attempt:wire.Attempt,Fence:wire.Fence,Operation:wire.Operation,
  Version:wire.Version,InputDigest:wire.InputDigest,
  ResultDigest:wire.ResultDigest,Params:wire.Params,
  Inputs:wire.Inputs,Outputs:wire.Outputs,Metrics:wire.Metrics,
  ReviewRequired:wire.ReviewRequired}
 if normalizeCanonicalSigningTask(&result)!=nil{return CanonicalSigningTask{},ErrInvalid}
 if _,err:=canonicalTaskOutputRefs(result.Outputs);err!=nil{return CanonicalSigningTask{},err}
 return result,nil
}
