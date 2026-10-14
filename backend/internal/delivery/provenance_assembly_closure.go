package delivery

import (
 "bytes"
 "encoding/json"
 "fmt"
 "io"
 "sort"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// SignedAssemblyEntry binds one frozen ready slot to one adopted V31 result.
// This document is a proposed input to atomic SQL admission, not authority by
// itself: receipts and live policies must be rechecked at commit time.
type SignedAssemblyEntry struct {
 Ordinal int `json:"ordinal"`
 PresetID uuid.UUID `json:"preset_id"`
 ExportID uuid.UUID `json:"export_id"`
 Export v2input.Ref `json:"export"`
 SigningKey string `json:"signing_key"`
 TaskID uuid.UUID `json:"task_id"`
 ResultID uuid.UUID `json:"result_id"`
 Signed v2input.Ref `json:"signed"`
 Manifest v2input.Ref `json:"manifest"`
 ValidationSHA string `json:"validation_sha"`
}

// MatchSignedAssemblyClosure accepts saved bytes only if they are the exact
// canonical closure reconstructed from current independently read evidence.
// Admission must still compare the closure SHA and reauthorize in SQL.
func MatchSignedAssemblyClosure(saved []byte,tenant,actor,parent,policy uuid.UUID,policySHA string,
 dependencies []SigningDependency,receipts []SignedDependencyReceipt)(SignedAssemblyClosure,error){
 if len(saved)<2||len(saved)>8<<20{return SignedAssemblyClosure{},v2input.ErrInvalid}
 decoder:=json.NewDecoder(bytes.NewReader(saved));decoder.DisallowUnknownFields()
 var decoded SignedAssemblyClosure
 if decoder.Decode(&decoded)!=nil||decoder.Decode(new(any))!=io.EOF{return SignedAssemblyClosure{},v2input.ErrInvalid}
 expected,raw,_,err:=CompileSignedAssemblyClosure(tenant,actor,parent,policy,policySHA,dependencies,receipts)
 if err!=nil||!bytes.Equal(saved,raw){return SignedAssemblyClosure{},v2input.ErrInvalid}
 return expected,nil
}

type SignedAssemblyClosure struct {
 SchemaVersion int `json:"schema_version"`
 TenantID uuid.UUID `json:"tenant_id"`
 ActorID uuid.UUID `json:"actor_id"`
 ParentID uuid.UUID `json:"parent_id"`
 PolicyID uuid.UUID `json:"policy_id"`
 PolicySHA string `json:"policy_sha"`
 Entries []SignedAssemblyEntry `json:"entries"`
}

// CompileSignedAssemblyClosureForSlots is the complete-matrix entry point.
// It derives the required signing set from frozen slots, so a caller cannot
// silently drop a ready export while presenting a self-consistent subset.
func CompileSignedAssemblyClosureForSlots(tenant,actor,parent,policy uuid.UUID,policySHA string,
 slots []Slot,dependencies []SigningDependency,receipts []SignedDependencyReceipt)(SignedAssemblyClosure,[]byte,string,error){
 invalid:=func()(SignedAssemblyClosure,[]byte,string,error){return SignedAssemblyClosure{},nil,"",v2input.ErrInvalid}
 expected,err:=CompileSigningDependencies(parent,policy,slots)
 if err!=nil||len(expected)!=len(dependencies)||len(expected)!=len(receipts){return invalid()}
 type pair struct{ordinal int;preset uuid.UUID}
 submitted:=make(map[pair]int,len(dependencies))
 for i,dependency:=range dependencies{
  key:=pair{dependency.Ordinal,dependency.PresetID}
  if _,exists:=submitted[key];exists{return invalid()}
  submitted[key]=i
 }
 ordered:=make([]SignedDependencyReceipt,len(expected))
 for i,dependency:=range expected{
  index,exists:=submitted[pair{dependency.Ordinal,dependency.PresetID}]
  if !exists||dependencies[index]!=dependency{return invalid()}
  ordered[i]=receipts[index]
 }
 return CompileSignedAssemblyClosure(tenant,actor,parent,policy,policySHA,expected,ordered)
}

// MatchSignedAssemblyClosureForSlots also rejects a saved closure that no
// longer represents the complete frozen slot matrix.
func MatchSignedAssemblyClosureForSlots(saved []byte,tenant,actor,parent,policy uuid.UUID,policySHA string,
 slots []Slot,dependencies []SigningDependency,receipts []SignedDependencyReceipt)(SignedAssemblyClosure,error){
 expected,raw,_,err:=CompileSignedAssemblyClosureForSlots(tenant,actor,parent,policy,policySHA,slots,dependencies,receipts)
 if err!=nil||!bytes.Equal(saved,raw){return SignedAssemblyClosure{},v2input.ErrInvalid}
 return expected,nil
}

// CompileSignedAssemblyClosure requires an exact one-to-one set. Callers must
// pass receipts returned by VerifySigningDependencyCurrent, not client data.
// The returned bytes are owned and canonical; the SHA binds admission to them.
func CompileSignedAssemblyClosure(tenant,actor,parent,policy uuid.UUID,policySHA string,
 dependencies []SigningDependency,receipts []SignedDependencyReceipt)(SignedAssemblyClosure,[]byte,string,error){
 invalid:=func()(SignedAssemblyClosure,[]byte,string,error){return SignedAssemblyClosure{},nil,"",v2input.ErrInvalid}
 if tenant==uuid.Nil||actor==uuid.Nil||parent==uuid.Nil||policy==uuid.Nil||!shaPattern.MatchString(policySHA)||len(dependencies)==0||
  len(dependencies)>80000||len(dependencies)!=len(receipts){return invalid()}
 type pair struct{ordinal int;preset uuid.UUID}
 bySlot:=map[pair]SignedAssemblyEntry{}
 seenExportIDs:=map[uuid.UUID]bool{}
 seenArtifacts:=map[uuid.UUID]bool{}
 seenObjectKeys:=map[string]bool{}
 seenTasks:=map[uuid.UUID]bool{}
 seenResults:=map[uuid.UUID]bool{}
 seenKeys:=map[string]bool{}
 for i,dependency:=range dependencies{
  receipt:=receipts[i]
  slot:=pair{dependency.Ordinal,dependency.PresetID}
  expectedKey:=uuid.NewSHA1(parent,[]byte(fmt.Sprintf("%d:%s",dependency.Ordinal,dependency.PresetID))).String()
  workflow:=uuid.NewSHA1(tenant,[]byte("provenance:"+actor.String()+":"+expectedKey))
  expectedTask:=uuid.NewSHA1(workflow,[]byte("provenance.sign:0"))
  if dependency.Ordinal<0||dependency.Ordinal>4999||dependency.PresetID==uuid.Nil||
   dependency.PolicyID!=policy||dependency.ExportID==uuid.Nil||dependency.Export.ArtifactID==uuid.Nil||
   !shaPattern.MatchString(dependency.Export.SHA256)||dependency.IdempotencyKey!=expectedKey||
   !validSignedDependencyReceipt(dependency,receipt)||receipt.TaskID!=expectedTask||seenExportIDs[dependency.ExportID]||
   seenTasks[receipt.TaskID]||seenResults[receipt.ResultID]||seenKeys[expectedKey]{return invalid()}
  for _,id:=range []uuid.UUID{dependency.Export.ArtifactID,receipt.Signed.ArtifactID,receipt.Manifest.ArtifactID}{
   if seenArtifacts[id]{return invalid()};seenArtifacts[id]=true
  }
  for _,key:=range []string{receipt.SignedObject.Key,receipt.ManifestObject.Key}{
   if seenObjectKeys[key]{return invalid()};seenObjectKeys[key]=true
  }
  if _,exists:=bySlot[slot];exists{return invalid()}
  bySlot[slot]=SignedAssemblyEntry{dependency.Ordinal,dependency.PresetID,dependency.ExportID,
   dependency.Export,expectedKey,receipt.TaskID,receipt.ResultID,receipt.Signed,receipt.Manifest,receipt.ValidationSHA}
  seenExportIDs[dependency.ExportID]=true;seenTasks[receipt.TaskID]=true
  seenResults[receipt.ResultID]=true;seenKeys[expectedKey]=true
 }
 entries:=make([]SignedAssemblyEntry,0,len(bySlot))
 for _,entry:=range bySlot{entries=append(entries,entry)}
 sort.Slice(entries,func(i,j int)bool{if entries[i].Ordinal!=entries[j].Ordinal{return entries[i].Ordinal<entries[j].Ordinal};return entries[i].PresetID.String()<entries[j].PresetID.String()})
 closure:=SignedAssemblyClosure{1,tenant,actor,parent,policy,policySHA,entries}
 raw,err:=canonical.Marshal(closure);if err!=nil||len(raw)>8<<20{return invalid()}
 return closure,raw,digest(raw),nil
}
