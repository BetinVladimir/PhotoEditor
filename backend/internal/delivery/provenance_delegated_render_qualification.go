package delivery

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type DelegatedRenderQualificationCatalog struct {
 Tasks SQLDelegatedTaskCatalog
 Adopted DelegatedAdoptedResultReader
}

func(s DelegatedRenderQualificationCatalog)ReadDelegatedRenderQualification(
 ctx context.Context,task provenance.CanonicalSigningTask,
 recipe v2draft.FrozenRenderRecipe)(provenance.SigningRenderQualification,error){
 var zero provenance.SigningRenderQualification
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Tasks.DB==nil||s.Adopted.Tasks.DB!=s.Tasks.DB||
  s.Adopted.Tasks.Lease!=s.Tasks.Lease||
  s.Adopted.Tasks.Scope!=s.Tasks.Scope||
  s.Adopted.Tasks.Ready!=s.Tasks.Ready||
  s.Adopted.Tasks.Slot.Ordinal!=s.Tasks.Slot.Ordinal||
  s.Adopted.Tasks.Slot.PresetID!=s.Tasks.Slot.PresetID||
  s.Adopted.Tasks.Bucket!=s.Tasks.Bucket||
  task.Task==uuid.Nil||task.Operation!="render.compose"||task.Version!="1"{
  return zero,v2input.ErrInvalid
 }
 var roots []v2input.Ref
 if json.Unmarshal(task.Outputs,&roots)!=nil||len(roots)!=1||
  roots[0].ArtifactID==uuid.Nil||!shaPattern.MatchString(roots[0].SHA256){
  return zero,v2input.ErrInvalid
 }
 taskRaw,err:=canonical.Marshal(task)
 if err!=nil{return zero,err}
 check:=func()error{
  current,err:=s.Tasks.ReadDelegatedTask(ctx,task.Task)
  if err!=nil{return err}
  currentRaw,err:=canonical.Marshal(current)
  if err!=nil||!bytes.Equal(currentRaw,taskRaw){return v2input.ErrInvalid}
  root,_,err:=s.Adopted.ReadDelegatedAdoptedResult(ctx,task)
  if err!=nil{return err}
  if root!=roots[0]{return v2input.ErrInvalid}
  return ctx.Err()
 }
 if err:=check();err!=nil{return zero,err}
 var raw []byte
 err=s.Tasks.DB.QueryRow(ctx,`SELECT photo.v31_delivery_render_qualification(
  $1,$2,$3,$4,$5)`,s.Tasks.Lease.Expansion,s.Tasks.Lease.Token,
  s.Tasks.Slot.Ordinal,s.Tasks.Slot.PresetID,task.Task).Scan(&raw)
 if err!=nil{return zero,err}
 var wire struct{Pin json.RawMessage `json:"pin"`;Profile json.RawMessage `json:"profile"`}
 if len(raw)<2||len(raw)>1<<20||json.Unmarshal(raw,&wire)!=nil||
  len(wire.Pin)<2||len(wire.Profile)<2{return zero,v2input.ErrInvalid}
 qualification,err:=provenance.DecodeSigningRenderQualification(wire.Pin,
  recipe.Scalars.ModelLockSHA,recipe.Scalars.ICCLockSHA,wire.Profile)
 if err!=nil{return zero,err}
 if err:=check();err!=nil{return zero,err}
 return qualification,nil
}

var _ provenance.DelegatedRenderQualificationReader=DelegatedRenderQualificationCatalog{}
