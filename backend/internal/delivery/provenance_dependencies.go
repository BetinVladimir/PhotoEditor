package delivery

import (
 "fmt"
 "sort"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// SigningDependency is the exact child export that must receive a V31
// independently verified signature before its Delivery slot may be ready.
// The key is stable across retries of the same frozen parent and slot.
type SigningDependency struct {
 Ordinal int
 PresetID uuid.UUID
 ExportID uuid.UUID
 Export v2input.Ref
 PolicyID uuid.UUID
 IdempotencyKey string
}

// CompileSigningDependencies is a pure preflight. The caller must still obtain
// an authenticated, qualified signing policy, start each signer, and verify
// signed bytes and manifest under current authority before slot adoption.
func CompileSigningDependencies(parent, policy uuid.UUID, slots []Slot) ([]SigningDependency,error) {
 if parent==uuid.Nil||policy==uuid.Nil||len(slots)<1||len(slots)>80000{return nil,v2input.ErrInvalid}
 seenSlots:=map[string]bool{}
 seenExports:=map[uuid.UUID]bool{}
 seenArtifacts:=map[uuid.UUID]bool{}
 out:=make([]SigningDependency,0,len(slots))
 for _,slot:=range slots {
  if slot.Ordinal<0||slot.Ordinal>4999||slot.PresetID==uuid.Nil||!shaPattern.MatchString(slot.PresetSHA)||
   slot.Binding.AssetVersionID==uuid.Nil||slot.Binding.Source.ArtifactID==uuid.Nil||
   !shaPattern.MatchString(slot.Binding.Source.SHA256)||!shaPattern.MatchString(slot.Binding.CanvasHash){return nil,v2input.ErrInvalid}
  pair:=fmt.Sprintf("%d:%s",slot.Ordinal,slot.PresetID)
  if seenSlots[pair]{return nil,v2input.ErrInvalid};seenSlots[pair]=true
  switch slot.State {
  case "omitted":
   if slot.Required||slot.ExportID!=nil||slot.Export!=nil||slot.Reason==nil||*slot.Reason==""{return nil,v2input.ErrInvalid}
   continue
  case "ready":
   if slot.ExportID==nil||*slot.ExportID==uuid.Nil||slot.Export==nil||slot.Export.ArtifactID==uuid.Nil||!shaPattern.MatchString(slot.Export.SHA256)||slot.Reason!=nil{return nil,v2input.ErrInvalid}
  default:return nil,v2input.ErrInvalid
  }
  if seenExports[*slot.ExportID]||seenArtifacts[slot.Export.ArtifactID]{return nil,v2input.ErrInvalid}
  seenExports[*slot.ExportID]=true;seenArtifacts[slot.Export.ArtifactID]=true
  key:=uuid.NewSHA1(parent,[]byte(pair)).String()
  out=append(out,SigningDependency{Ordinal:slot.Ordinal,PresetID:slot.PresetID,ExportID:*slot.ExportID,Export:*slot.Export,PolicyID:policy,IdempotencyKey:key})
 }
 if len(out)==0{return nil,v2input.ErrInvalid}
 sort.Slice(out,func(i,j int)bool{if out[i].Ordinal!=out[j].Ordinal{return out[i].Ordinal<out[j].Ordinal};return out[i].PresetID.String()<out[j].PresetID.String()})
 return out,nil
}
