package delivery

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

type DelegatedDirectImageProducer struct{Tasks SQLDelegatedTaskCatalog}

type delegatedProducerEvidence struct{
 Task uuid.UUID `json:"task"`
 Attempt int `json:"attempt"`
 Fence int64 `json:"fence"`
}

func(s DelegatedDirectImageProducer)lookup(ctx context.Context,
 ref v2input.Ref)(delegatedProducerEvidence,error){
 var value delegatedProducerEvidence
 if err:=ctx.Err();err!=nil{return value,err}
 if s.Tasks.DB==nil||ref.ArtifactID==uuid.Nil||
  !shaPattern.MatchString(ref.SHA256){return value,v2input.ErrInvalid}
 var raw []byte
 err:=s.Tasks.DB.QueryRow(ctx,`SELECT photo.v31_delivery_direct_producer(
  $1,$2,$3,$4,$5,$6)`,s.Tasks.Lease.Expansion,s.Tasks.Lease.Token,
  s.Tasks.Slot.Ordinal,s.Tasks.Slot.PresetID,ref.ArtifactID,ref.SHA256).Scan(&raw)
 if err!=nil{return value,err}
 if len(raw)==0{return value,pgx.ErrNoRows}
 if len(raw)<2||len(raw)>1024||json.Unmarshal(raw,&value)!=nil||
  value.Task==uuid.Nil||value.Attempt<1||value.Fence<1{
  return delegatedProducerEvidence{},v2input.ErrInvalid
 }
 return value,nil
}

// ResolveDelegatedDirectImageProducer proves a declared, fenced task output.
// An adopted render/negative WorkingImage uses a distinct child resolver.
func(s DelegatedDirectImageProducer)ResolveDelegatedDirectImageProducer(
 ctx context.Context,ref v2input.Ref)(uuid.UUID,error){
 if err:=ctx.Err();err!=nil{return uuid.Nil,err}
 if s.Tasks.DB==nil{return uuid.Nil,v2input.ErrInvalid}
 final,err:=s.Tasks.ReadDelegatedTask(ctx,s.Tasks.Ready.Task)
 if err!=nil{return uuid.Nil,err}
 finalRaw,err:=canonical.Marshal(final)
 if err!=nil{return uuid.Nil,v2input.ErrInvalid}
 candidate,err:=s.lookup(ctx,ref)
 if err!=nil{return uuid.Nil,err}
 if candidate.Task==s.Tasks.Ready.Task{return uuid.Nil,v2input.ErrInvalid}
 task,err:=s.Tasks.ReadDelegatedTask(ctx,candidate.Task)
 if err!=nil{return uuid.Nil,err}
 if task.Attempt!=candidate.Attempt||task.Fence!=candidate.Fence||
  !delegatedTaskHasOutput(task,ref){return uuid.Nil,v2input.ErrInvalid}
 taskRaw,err:=canonical.Marshal(task)
 if err!=nil{return uuid.Nil,v2input.ErrInvalid}
 currentCandidate,err:=s.lookup(ctx,ref)
 if err!=nil{return uuid.Nil,err}
 if currentCandidate!=candidate{return uuid.Nil,v2input.ErrInvalid}
 currentTask,err:=s.Tasks.ReadDelegatedTask(ctx,candidate.Task)
 if err!=nil{return uuid.Nil,err}
 currentTaskRaw,err:=canonical.Marshal(currentTask)
 if err!=nil||!bytes.Equal(taskRaw,currentTaskRaw){return uuid.Nil,v2input.ErrInvalid}
 currentFinal,err:=s.Tasks.ReadDelegatedTask(ctx,s.Tasks.Ready.Task)
 if err!=nil{return uuid.Nil,err}
 currentFinalRaw,err:=canonical.Marshal(currentFinal)
 if err!=nil||!bytes.Equal(finalRaw,currentFinalRaw){return uuid.Nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return uuid.Nil,err}
 return candidate.Task,nil
}

func delegatedTaskHasOutput(task provenance.CanonicalSigningTask,ref v2input.Ref)bool{
 var outputs []v2input.Ref
 if json.Unmarshal(task.Outputs,&outputs)!=nil{return false}
 for _,output:=range outputs{if output==ref{return true}}
 return false
}
