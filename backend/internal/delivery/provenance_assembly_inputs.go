package delivery

import (
 "encoding/json"
 "sort"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// SignedAssemblyRequiredRoots derives the document and signed-object inputs
// from an exact full-slot closure. Transitive WorkingImage/recipe roots still
// need independent resolution by the parent assembly coordinator.
func SignedAssemblyRequiredRoots(assemblyRef v2input.Ref,assemblyRaw []byte,
 shardRaw [][]byte,closureRef v2input.Ref,closureRaw []byte,envelopeRef v2input.Ref,
 tenant,actor,parent uuid.UUID,slots []Slot,dependencies []SigningDependency,
 receipts []SignedDependencyReceipt)([]v2input.Entry,error){
 if envelopeRef.ArtifactID==uuid.Nil||!shaPattern.MatchString(envelopeRef.SHA256){return nil,v2input.ErrInvalid}
 _,envelopeRaw,envelopeSHA,err:=CompileSignedAssemblyEnvelope(assemblyRef,assemblyRaw,shardRaw,
  closureRef,closureRaw,tenant,actor,parent,slots,dependencies,receipts)
 if err!=nil||envelopeSHA!=envelopeRef.SHA256||len(envelopeRaw)<2{return nil,v2input.ErrInvalid}
 var assembly AssemblyInput;var closure SignedAssemblyClosure
 if json.Unmarshal(assemblyRaw,&assembly)!=nil||json.Unmarshal(closureRaw,&closure)!=nil{return nil,v2input.ErrInvalid}
 roots:=[]v2input.Entry{
  {Ref:envelopeRef,Contract:"SignedDeliveryAssemblyEnvelope"},
  {Ref:assemblyRef,Contract:"DeliveryAssemblyInput"},
  {Ref:closureRef,Contract:"SignedDeliveryAssemblyClosure"},
 }
 for _,ref:=range assembly.Shards{roots=append(roots,v2input.Entry{Ref:ref,Contract:"DeliverySlotShard"})}
 for _,preset:=range assembly.Presets{roots=append(roots,v2input.Entry{Ref:preset.Config,Contract:"ExportPresetConfig"})}
 for _,entry:=range closure.Entries{
  roots=append(roots,v2input.Entry{Ref:entry.Export,Contract:"EncodedExport"},
   v2input.Entry{Ref:entry.Signed,Contract:"SignedExport"},
   v2input.Entry{Ref:entry.Manifest,Contract:"C2PAManifest"})
 }
 seen:=make(map[uuid.UUID]bool,len(roots))
 for _,entry:=range roots{
  if entry.Ref.ArtifactID==uuid.Nil||!shaPattern.MatchString(entry.Ref.SHA256)||seen[entry.Ref.ArtifactID]{return nil,v2input.ErrInvalid}
  seen[entry.Ref.ArtifactID]=true
 }
 sort.Slice(roots,func(i,j int)bool{return roots[i].Ref.ArtifactID.String()<roots[j].Ref.ArtifactID.String()})
 return roots,nil
}
