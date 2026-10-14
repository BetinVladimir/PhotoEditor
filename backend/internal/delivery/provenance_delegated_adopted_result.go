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

type DelegatedAdoptedResultReader struct{
 Tasks SQLDelegatedTaskCatalog
 Documents DelegatedTaskDocumentReader
}

// ReadDelegatedAdoptedResult binds a physical ImageResult root to the current
// canonical render/negative task and its deterministic WorkingImage child.
func(s DelegatedAdoptedResultReader)ReadDelegatedAdoptedResult(ctx context.Context,
 task provenance.CanonicalSigningTask)(v2input.Ref,v2draft.ImageResultDocument,error){
 var noRef v2input.Ref
 var noResult v2draft.ImageResultDocument
 if err:=ctx.Err();err!=nil{return noRef,noResult,err}
 if s.Tasks.DB==nil||s.Documents.Objects==nil||
  s.Documents.Tasks.DB!=s.Tasks.DB||
  s.Documents.Tasks.Lease!=s.Tasks.Lease||
  s.Documents.Tasks.Scope!=s.Tasks.Scope||
  s.Documents.Tasks.Ready!=s.Tasks.Ready||
  s.Documents.Tasks.Slot.Ordinal!=s.Tasks.Slot.Ordinal||
  s.Documents.Tasks.Slot.PresetID!=s.Tasks.Slot.PresetID||
  s.Documents.Tasks.Bucket!=s.Tasks.Bucket||
  task.Task==uuid.Nil||task.Version!="1"||
  task.Operation!="render.compose"&&task.Operation!="optical.negative"{
  return noRef,noResult,v2input.ErrInvalid
 }
 var outputs []v2input.Ref
 if json.Unmarshal(task.Outputs,&outputs)!=nil||len(outputs)!=1{
  return noRef,noResult,v2input.ErrInvalid
 }
 root:=outputs[0]
 currentTask,err:=s.Tasks.ReadDelegatedTask(ctx,task.Task)
 if err!=nil{return noRef,noResult,err}
 taskRaw,err:=canonical.Marshal(task)
 if err!=nil{return noRef,noResult,v2input.ErrInvalid}
 currentRaw,err:=canonical.Marshal(currentTask)
 if err!=nil||!bytes.Equal(taskRaw,currentRaw){
  return noRef,noResult,v2input.ErrInvalid
 }
 raw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,root,1<<20)
 if err!=nil{return noRef,noResult,err}
 result,err:=v2draft.ParseImageResult(raw,root)
 if err!=nil||result.SchemaVersion!=1||
  task.Operation=="optical.negative"&&result.GeneratedMask!=nil||
  result.WorkingImage.ArtifactID==root.ArtifactID||
  result.ChangedMask.ArtifactID==root.ArtifactID||
  result.Report.ArtifactID==root.ArtifactID||
  result.WorkingImage.ArtifactID==result.ChangedMask.ArtifactID||
  result.WorkingImage.ArtifactID==result.Report.ArtifactID||
  result.ChangedMask.ArtifactID==result.Report.ArtifactID{
  return noRef,noResult,v2input.ErrInvalid
 }
 adoption:=DelegatedAdoptedImageProducer{Tasks:s.Tasks,Documents:s.Documents}
 evidence,err:=adoption.lookup(ctx,result.WorkingImage)
 if err!=nil{return noRef,noResult,err}
 if evidence.Task!=task.Task||evidence.Attempt!=task.Attempt||
  evidence.Fence!=task.Fence||evidence.Operation!=task.Operation||
  evidence.RootArtifact!=root.ArtifactID||evidence.RootSHA!=root.SHA256{
  return noRef,noResult,v2input.ErrInvalid
 }
 repeated,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,root,1<<20)
 if err!=nil||!bytes.Equal(raw,repeated){return noRef,noResult,v2input.ErrInvalid}
 currentEvidence,err:=adoption.lookup(ctx,result.WorkingImage)
 if err!=nil||currentEvidence!=evidence{return noRef,noResult,v2input.ErrInvalid}
 currentTask,err=s.Tasks.ReadDelegatedTask(ctx,task.Task)
 if err!=nil{return noRef,noResult,err}
 currentRaw,err=canonical.Marshal(currentTask)
 if err!=nil||!bytes.Equal(taskRaw,currentRaw){
  return noRef,noResult,v2input.ErrInvalid
 }
 if err:=ctx.Err();err!=nil{return noRef,noResult,err}
 return root,result,nil
}
