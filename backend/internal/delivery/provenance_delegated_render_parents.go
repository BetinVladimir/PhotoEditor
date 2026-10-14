package delivery

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

type DelegatedRenderParentCatalog struct {
 Tasks SQLDelegatedTaskCatalog
 Adopted DelegatedAdoptedResultReader
}

func(s DelegatedRenderParentCatalog)ReadDelegatedRenderParents(ctx context.Context,
 task provenance.CanonicalSigningTask)([]provenance.SigningRenderParent,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Tasks.DB==nil||s.Adopted.Tasks.DB!=s.Tasks.DB||
  s.Adopted.Tasks.Lease!=s.Tasks.Lease||
  s.Adopted.Tasks.Scope!=s.Tasks.Scope||
  s.Adopted.Tasks.Ready!=s.Tasks.Ready||
  s.Adopted.Tasks.Slot.Ordinal!=s.Tasks.Slot.Ordinal||
  s.Adopted.Tasks.Slot.PresetID!=s.Tasks.Slot.PresetID||
  s.Adopted.Tasks.Bucket!=s.Tasks.Bucket||
  task.Task==uuid.Nil||task.Operation!="render.compose"||task.Version!="1"{
  return nil,v2input.ErrInvalid
 }
 expected,err:=canonical.Marshal(task)
 if err!=nil{return nil,err}
 check:=func()error{
  current,err:=s.Tasks.ReadDelegatedTask(ctx,task.Task)
  if err!=nil{return err}
  actual,err:=canonical.Marshal(current)
  if err!=nil||!bytes.Equal(expected,actual){return v2input.ErrInvalid}
  _,_,err=s.Adopted.ReadDelegatedAdoptedResult(ctx,task)
  return err
 }
 if err:=check();err!=nil{return nil,err}
 var raw []byte
 err=s.Tasks.DB.QueryRow(ctx,`SELECT photo.v31_delivery_render_parents(
  $1,$2,$3,$4,$5)`,s.Tasks.Lease.Expansion,s.Tasks.Lease.Token,
  s.Tasks.Slot.Ordinal,s.Tasks.Slot.PresetID,task.Task).Scan(&raw)
 if err!=nil{return nil,err}
 var rows []struct{
  ArtifactID uuid.UUID `json:"artifact_id"`
  SHA256 string `json:"sha256"`
  Producer uuid.UUID `json:"producer"`
 }
 if len(raw)<2||len(raw)>16<<10||json.Unmarshal(raw,&rows)!=nil||
  rows==nil||len(rows)>33{return nil,v2input.ErrInvalid}
 parents:=make([]provenance.SigningRenderParent,0,len(rows))
 seen:=map[uuid.UUID]bool{}
 for _,row:=range rows{
  ref:=v2input.Ref{ArtifactID:row.ArtifactID,SHA256:row.SHA256}
  if ref.ArtifactID==uuid.Nil||!shaPattern.MatchString(ref.SHA256)||
   seen[ref.ArtifactID]||row.Producer==uuid.Nil||row.Producer==task.Task{
   return nil,v2input.ErrInvalid
  }
  seen[ref.ArtifactID]=true
  producer,err:=s.Tasks.ReadDelegatedTask(ctx,row.Producer)
  if err!=nil{return nil,err}
  if !delegatedTaskHasOnlyOutput(producer,ref){return nil,v2input.ErrInvalid}
  parents=append(parents,provenance.SigningRenderParent{Root:ref,Producer:producer})
 }
 if err:=check();err!=nil{return nil,err}
 var current []byte
 err=s.Tasks.DB.QueryRow(ctx,`SELECT photo.v31_delivery_render_parents(
  $1,$2,$3,$4,$5)`,s.Tasks.Lease.Expansion,s.Tasks.Lease.Token,
  s.Tasks.Slot.Ordinal,s.Tasks.Slot.PresetID,task.Task).Scan(&current)
 if err!=nil{return nil,err}
 first,err:=canonical.Bytes(raw)
 if err!=nil{return nil,err}
 repeated,err:=canonical.Bytes(current)
 if err!=nil||!bytes.Equal(first,repeated){return nil,v2input.ErrInvalid}
 return parents,ctx.Err()
}

var _ provenance.DelegatedRenderParentReader=DelegatedRenderParentCatalog{}
