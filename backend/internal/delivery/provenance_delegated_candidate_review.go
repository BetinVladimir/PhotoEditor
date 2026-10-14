package delivery

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type DelegatedCandidateReviewReader struct{
 Tasks SQLDelegatedTaskCatalog
 Documents DelegatedTaskDocumentReader
}

type DelegatedCandidateApproval struct{
 ID,Task,Result uuid.UUID
 Root,Report v2input.Ref
 Accepted bool
}

type delegatedCandidateReviewRow struct{
 Count int `json:"count"`
 ID uuid.UUID `json:"id"`
 Revision int64 `json:"revision"`
 State string `json:"state"`
 Kind string `json:"kind"`
 Decision string `json:"decision"`
 Route string `json:"route"`
 DecisionHash string `json:"decision_hash"`
 Actor *uuid.UUID `json:"actor"`
 Dated bool `json:"dated"`
 Selection json.RawMessage `json:"selection"`
 Candidate uuid.UUID `json:"candidate_artifact_id"`
 CandidateSHA string `json:"candidate_sha"`
 Base *uuid.UUID `json:"base_artifact_id"`
 BaseSHA *string `json:"base_sha"`
}

func(s DelegatedCandidateReviewReader)lookup(ctx context.Context,
 task provenance.CanonicalSigningTask,candidate v2input.Ref)(delegatedCandidateReviewRow,error){
 var row delegatedCandidateReviewRow
 if err:=ctx.Err();err!=nil{return row,err}
 if s.Tasks.DB==nil||task.Task==uuid.Nil||candidate.ArtifactID==uuid.Nil||
  !shaPattern.MatchString(candidate.SHA256){return row,v2input.ErrInvalid}
 var raw []byte
 err:=s.Tasks.DB.QueryRow(ctx,`SELECT photo.v31_delivery_candidate_review(
  $1,$2,$3,$4,$5,$6,$7)`,s.Tasks.Lease.Expansion,s.Tasks.Lease.Token,
  s.Tasks.Slot.Ordinal,s.Tasks.Slot.PresetID,task.Task,
  candidate.ArtifactID,candidate.SHA256).Scan(&raw)
 if err!=nil{return row,err}
 if len(raw)<2||len(raw)>8192||json.Unmarshal(raw,&row)!=nil||
  row.Count<0||row.Count>2{return delegatedCandidateReviewRow{},v2input.ErrInvalid}
 return row,nil
}

func(s DelegatedCandidateReviewReader)ReadDelegatedCandidateReview(
 ctx context.Context,task provenance.CanonicalSigningTask,
 root,report v2input.Ref,required bool)(*DelegatedCandidateApproval,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Tasks.DB==nil||s.Documents.Tasks.DB!=s.Tasks.DB||
  s.Documents.Tasks.Lease!=s.Tasks.Lease||
  s.Documents.Tasks.Scope!=s.Tasks.Scope||
  s.Documents.Tasks.Ready!=s.Tasks.Ready||
  s.Documents.Tasks.Slot.Ordinal!=s.Tasks.Slot.Ordinal||
  s.Documents.Tasks.Slot.PresetID!=s.Tasks.Slot.PresetID||
  s.Documents.Tasks.Bucket!=s.Tasks.Bucket||
  s.Documents.Objects==nil||task.Task==uuid.Nil||
  task.Operation!="render.compose"&&task.Operation!="optical.negative"||
  task.Version!="1"||task.Operation=="optical.negative"&&
   (!required||!task.ReviewRequired){return nil,v2input.ErrInvalid}
 if !delegatedTaskHasOnlyOutput(task,root){return nil,v2input.ErrInvalid}
 rootRaw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,root,1<<20)
 if err!=nil{return nil,err}
 result,err:=v2draft.ParseImageResult(rootRaw,root)
 if err!=nil||result.Report!=report||
  task.Operation=="optical.negative"&&result.GeneratedMask!=nil{
  return nil,v2input.ErrInvalid
 }
 candidate:=result.WorkingImage
 var expectedBase *v2input.Ref
 var paramsRaw []byte
 if task.Operation=="optical.negative"{
  paramsRaw,err=s.Documents.ReadDelegatedTaskDocument(ctx,task,task.Params,65536)
  if err!=nil{return nil,err}
  params,err:=v2optical.ParseNegativeParams(paramsRaw)
  if err!=nil{return nil,v2input.ErrInvalid}
  base:=params.Base.Source
  expectedBase=&base
 }
 row,err:=s.lookup(ctx,task,candidate)
 if err!=nil{return nil,err}
 if row.Count==2||row.Count==0&&required{return nil,v2input.ErrInvalid}
 var approval *DelegatedCandidateApproval
 if row.Count==1{
  if row.ID==uuid.Nil||row.Revision!=2||row.State!="decided"||
   row.Kind!=task.Operation||row.Decision!="accept"||
   row.Route!="candidate"||row.Actor==nil||*row.Actor==uuid.Nil||
   !row.Dated||row.Base==nil||row.BaseSHA==nil||
   row.Candidate!=candidate.ArtifactID||row.CandidateSHA!=candidate.SHA256||
   expectedBase!=nil&&(*row.Base!=expectedBase.ArtifactID||
    *row.BaseSHA!=expectedBase.SHA256){return nil,v2input.ErrInvalid}
  snapshot:=review.CandidateSnapshot{TenantID:s.Tasks.Ready.Tenant,
   ReviewID:row.ID,RunID:task.Run,Revision:1,State:"pending",
   Kind:row.Kind,Candidate:review.ArtifactRef{ID:row.Candidate,
    SHA256:row.CandidateSHA},Base:review.ArtifactRef{
    ID:*row.Base,SHA256:*row.BaseSHA}}
  plan,err:=review.PlanCandidate(snapshot,*row.Actor,review.Decision{
   ExpectedRevision:1,CandidateHash:candidate.SHA256,Decision:"accept"})
  if err!=nil||plan.DecisionHash!=row.DecisionHash{return nil,v2input.ErrInvalid}
  expected,err:=canonical.Marshal(plan.Selection)
  if err!=nil{return nil,err}
  actual,err:=canonical.Bytes(row.Selection)
  if err!=nil||!bytes.Equal(actual,expected){return nil,v2input.ErrInvalid}
  var object map[string]json.RawMessage
  if json.Unmarshal(actual,&object)!=nil||object==nil{return nil,v2input.ErrInvalid}
  approval=&DelegatedCandidateApproval{ID:row.ID,Task:task.Task,
   Result:task.Result,Root:root,Report:report,Accepted:true}
 }
 currentRoot,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,root,1<<20)
 if err!=nil||!bytes.Equal(currentRoot,rootRaw){return nil,v2input.ErrInvalid}
 if expectedBase!=nil{
  currentParams,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,
   task.Params,65536)
  if err!=nil||!bytes.Equal(currentParams,paramsRaw){return nil,v2input.ErrInvalid}
 }
 currentRow,err:=s.lookup(ctx,task,candidate)
 if err!=nil{return nil,err}
 first,err:=canonical.Marshal(row)
 if err!=nil{return nil,err}
 current,err:=canonical.Marshal(currentRow)
 if err!=nil||!bytes.Equal(first,current){return nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return nil,err}
 return approval,nil
}
