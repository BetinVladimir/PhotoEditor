package delivery

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type DelegatedAdoptedImageProducer struct{
 Tasks SQLDelegatedTaskCatalog
 Documents DelegatedTaskDocumentReader
}

type delegatedAdoptedProducerEvidence struct{
 Task uuid.UUID `json:"task"`
 Attempt int `json:"attempt"`
 Fence int64 `json:"fence"`
 Operation string `json:"operation"`
 RootArtifact uuid.UUID `json:"root_artifact"`
 RootSHA string `json:"root_sha"`
}

func(s DelegatedAdoptedImageProducer)lookup(ctx context.Context,
 image v2input.Ref)(delegatedAdoptedProducerEvidence,error){
 var value delegatedAdoptedProducerEvidence
 if err:=ctx.Err();err!=nil{return value,err}
 if s.Tasks.DB==nil||image.ArtifactID==uuid.Nil||
  !shaPattern.MatchString(image.SHA256){return value,v2input.ErrInvalid}
 var raw []byte
 err:=s.Tasks.DB.QueryRow(ctx,`SELECT photo.v31_delivery_adopted_image_producer(
  $1,$2,$3,$4,$5,$6)`,s.Tasks.Lease.Expansion,s.Tasks.Lease.Token,
  s.Tasks.Slot.Ordinal,s.Tasks.Slot.PresetID,image.ArtifactID,image.SHA256).Scan(&raw)
 if err!=nil{return value,err}
 if len(raw)==0{return value,pgx.ErrNoRows}
 if len(raw)<2||len(raw)>2048||json.Unmarshal(raw,&value)!=nil||
  value.Task==uuid.Nil||value.Attempt<1||value.Fence<1||
  value.Operation!="render.compose"&&value.Operation!="optical.negative"||
  value.RootArtifact==uuid.Nil||!shaPattern.MatchString(value.RootSHA){
  return delegatedAdoptedProducerEvidence{},v2input.ErrInvalid
 }
 return value,nil
}

func(s DelegatedAdoptedImageProducer)ResolveDelegatedAdoptedImageProducer(
 ctx context.Context,image v2input.Ref)(uuid.UUID,error){
 if err:=ctx.Err();err!=nil{return uuid.Nil,err}
 if s.Tasks.DB==nil||s.Documents.Tasks.DB!=s.Tasks.DB||
  s.Documents.Tasks.Lease!=s.Tasks.Lease||
  s.Documents.Tasks.Scope!=s.Tasks.Scope||
  s.Documents.Tasks.Ready!=s.Tasks.Ready||
  s.Documents.Tasks.Slot.Ordinal!=s.Tasks.Slot.Ordinal||
  s.Documents.Tasks.Slot.PresetID!=s.Tasks.Slot.PresetID||
  s.Documents.Tasks.Bucket!=s.Tasks.Bucket{return uuid.Nil,v2input.ErrInvalid}
 candidate,err:=s.lookup(ctx,image)
 if err!=nil{return uuid.Nil,err}
 if candidate.Task==s.Tasks.Ready.Task{return uuid.Nil,v2input.ErrInvalid}
 task,err:=s.Tasks.ReadDelegatedTask(ctx,candidate.Task)
 if err!=nil{return uuid.Nil,err}
 if task.Task!=candidate.Task||task.Attempt!=candidate.Attempt||
  task.Fence!=candidate.Fence||task.Operation!=candidate.Operation||
  task.Version!="1"{return uuid.Nil,v2input.ErrInvalid}
 root:=v2input.Ref{ArtifactID:candidate.RootArtifact,SHA256:candidate.RootSHA}
 if !delegatedTaskHasOnlyOutput(task,root){return uuid.Nil,v2input.ErrInvalid}
 taskRaw,err:=canonical.Marshal(task)
 if err!=nil{return uuid.Nil,v2input.ErrInvalid}
 raw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,root,1<<20)
 if err!=nil{return uuid.Nil,err}
 result,err:=v2draft.ParseImageResult(raw,root)
 if err!=nil||result.WorkingImage!=image||
  candidate.Operation=="optical.negative"&&result.GeneratedMask!=nil{
  return uuid.Nil,v2input.ErrInvalid
 }
 repeated,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,root,1<<20)
 if err!=nil||!bytes.Equal(raw,repeated){return uuid.Nil,v2input.ErrInvalid}
 currentCandidate,err:=s.lookup(ctx,image)
 if err!=nil||currentCandidate!=candidate{return uuid.Nil,v2input.ErrInvalid}
 currentTask,err:=s.Tasks.ReadDelegatedTask(ctx,candidate.Task)
 if err!=nil{return uuid.Nil,err}
 currentRaw,err:=canonical.Marshal(currentTask)
 if err!=nil||!bytes.Equal(taskRaw,currentRaw){return uuid.Nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return uuid.Nil,err}
 return candidate.Task,nil
}

func delegatedTaskHasOnlyOutput(task provenance.CanonicalSigningTask,
 root v2input.Ref)bool{
 var outputs []v2input.Ref
 return json.Unmarshal(task.Outputs,&outputs)==nil&&len(outputs)==1&&outputs[0]==root
}
