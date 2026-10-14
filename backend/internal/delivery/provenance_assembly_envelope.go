package delivery

import (
 "bytes"
 "encoding/json"
 "sort"
 "strings"
 "unicode/utf8"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// SignedAssemblyEnvelope is a bounded root for the required-provenance path.
// The ordinary V30 assembly and the V31 closure remain separate immutable
// documents; admission must bind both refs to one task before activation.
type SignedAssemblyEnvelope struct {
 SchemaVersion int `json:"schema_version"`
 Assembly v2input.Ref `json:"assembly"`
 SigningClosure v2input.Ref `json:"signing_closure"`
}

// CompileSignedAssemblyEnvelope binds canonical required-mode assembly bytes
// to the exact complete signing closure. It does not publish either document or
// assert SQL/physical authority, which remains the coordinator's duty.
func CompileSignedAssemblyEnvelope(assemblyRef v2input.Ref,assemblyRaw []byte,
 shardRaw [][]byte,closureRef v2input.Ref,closureRaw []byte,tenant,actor,parent uuid.UUID,
 slots []Slot,dependencies []SigningDependency,receipts []SignedDependencyReceipt)(SignedAssemblyEnvelope,[]byte,string,error){
 invalid:=func()(SignedAssemblyEnvelope,[]byte,string,error){return SignedAssemblyEnvelope{},nil,"",v2input.ErrInvalid}
 if assemblyRef.ArtifactID==uuid.Nil||closureRef.ArtifactID==uuid.Nil||assemblyRef.ArtifactID==closureRef.ArtifactID||
  !shaPattern.MatchString(assemblyRef.SHA256)||!shaPattern.MatchString(closureRef.SHA256)||
  len(assemblyRaw)<2||len(assemblyRaw)>1<<20||len(closureRaw)<2||len(closureRaw)>8<<20||
  digest(assemblyRaw)!=assemblyRef.SHA256||digest(closureRaw)!=closureRef.SHA256{return invalid()}
 var assembly AssemblyInput
 if json.Unmarshal(assemblyRaw,&assembly)!=nil||assembly.SchemaVersion!=1||
  assembly.Policy.Provenance!="required"||assembly.ItemCount<1||assembly.ItemCount>5000||
  assembly.SlotCount<1||assembly.SlotCount>80000||assembly.SlotCount!=len(slots)||
  assembly.Policy.SigningPolicyID==uuid.Nil||!shaPattern.MatchString(assembly.Policy.SigningPolicySHA){return invalid()}
 exactAssembly,err:=canonical.Marshal(assembly)
 if err!=nil||!bytes.Equal(exactAssembly,assemblyRaw){return invalid()}
 if !signedAssemblyShardsMatch(assembly,slots,shardRaw){return invalid()}
 if _,err=MatchSignedAssemblyClosureForSlots(closureRaw,tenant,actor,parent,
  assembly.Policy.SigningPolicyID,assembly.Policy.SigningPolicySHA,slots,dependencies,receipts);err!=nil{return invalid()}
 envelope:=SignedAssemblyEnvelope{2,assemblyRef,closureRef}
 raw,err:=canonical.Marshal(envelope)
 if err!=nil||len(raw)>65536{return invalid()}
 return envelope,raw,digest(raw),nil
}

// signedAssemblyShardsMatch binds the claimed root refs to the actual frozen
// ready/omitted slot matrix. Snapshot/WorkingImage physical authority remains
// a separate parent preparation and SQL admission responsibility.
func signedAssemblyShardsMatch(assembly AssemblyInput,slots []Slot,shardRaw [][]byte)bool{
 if !shaPattern.MatchString(assembly.SnapshotDigest)||len(assembly.Presets)<1||len(assembly.Presets)>16||
  len(assembly.Shards)!=(len(slots)+499)/500||len(shardRaw)!=len(assembly.Shards)||
  assembly.ItemCount*len(assembly.Presets)!=len(slots){return false}
 presets:=append([]FrozenPreset(nil),assembly.Presets...)
 sort.Slice(presets,func(i,j int)bool{return presets[i].PresetID.String()<presets[j].PresetID.String()})
 required:=make(map[uuid.UUID]bool,len(assembly.Policy.RequiredPresets))
 chosen:=make(map[uuid.UUID]bool,len(presets))
 for _,id:=range assembly.Policy.RequiredPresets{if id==uuid.Nil||chosen[id]{return false};chosen[id]=true;required[id]=true}
 for _,id:=range assembly.Policy.OptionalPresets{if id==uuid.Nil||chosen[id]{return false};chosen[id]=true}
 if len(chosen)!=len(presets){return false}
 for i,preset:=range presets{
  if preset.PresetID==uuid.Nil||preset.PresetID!=assembly.Presets[i].PresetID||
   !chosen[preset.PresetID]||preset.Required!=required[preset.PresetID]||
   preset.Config.ArtifactID==uuid.Nil||preset.Config.SHA256!=preset.PresetSHA{return false}
 }
 ordered:=append([]Slot(nil),slots...)
 sort.Slice(ordered,func(i,j int)bool{if ordered[i].Ordinal!=ordered[j].Ordinal{return ordered[i].Ordinal<ordered[j].Ordinal};return ordered[i].PresetID.String()<ordered[j].PresetID.String()})
 var binding v2input.SessionVersionBinding
 versions:=make(map[uuid.UUID]bool,assembly.ItemCount)
 exportIDs:=make(map[uuid.UUID]bool,len(ordered))
 for i,slot:=range ordered{
  ordinal,presetIndex:=i/len(presets),i%len(presets)
  preset:=presets[presetIndex]
  if slot.Ordinal!=ordinal||slot.PresetID!=preset.PresetID||slot.PresetSHA!=preset.PresetSHA||
   slot.Required!=preset.Required||slot.Binding.AssetVersionID==uuid.Nil||
   slot.Binding.Source.ArtifactID==uuid.Nil||!shaPattern.MatchString(slot.Binding.Source.SHA256)||
   !shaPattern.MatchString(slot.Binding.CanvasHash){return false}
  if presetIndex==0{if versions[slot.Binding.AssetVersionID]{return false};versions[slot.Binding.AssetVersionID]=true;binding=slot.Binding
  }else if slot.Binding!=binding{return false}
  if slot.State=="ready"{
   if slot.ExportID==nil||*slot.ExportID==uuid.Nil||slot.Export==nil||slot.Export.ArtifactID==uuid.Nil||
    !shaPattern.MatchString(slot.Export.SHA256)||slot.Reason!=nil||exportIDs[*slot.ExportID]{return false}
   exportIDs[*slot.ExportID]=true
  }else if slot.State=="omitted"{
   if slot.Required||!assembly.Policy.AllowOmitted||slot.ExportID!=nil||slot.Export!=nil||slot.Reason==nil||
    !utf8.ValidString(*slot.Reason)||strings.TrimSpace(*slot.Reason)==""||utf8.RuneCountInString(*slot.Reason)>200{return false}
  }else{return false}
 }
 for index,ref:=range assembly.Shards{
  start:=index*500;end:=start+500;if end>len(ordered){end=len(ordered)}
  raw,err:=canonical.Marshal(SlotShard{1,assembly.SnapshotDigest,ordered[start:end]})
  if err!=nil||len(raw)>1<<20||ref.ArtifactID==uuid.Nil||digest(raw)!=ref.SHA256||
   !bytes.Equal(raw,shardRaw[index]){return false}
 }
 return true
}
