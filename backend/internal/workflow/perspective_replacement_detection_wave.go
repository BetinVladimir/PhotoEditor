package workflow

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type ReplacementDetectionAdmission struct {
 OriginalTask,Run,Task uuid.UUID
 RecoveredExisting bool
}
type ReplacementDetectionWavePass struct {
 WaitingForSettlement bool
 Total,Adopted,Ready,Publications int
 AutomaticReceipts []v2optical.AutomaticPerspectiveReplacementReceipt
 Unsupported []v2optical.NegativeReplacementNode
 Admissions []ReplacementDetectionAdmission
 Pending,Unconfirmed,Deferred []v2optical.NegativeReplacementNode
}
type ReplacementDetectionWaveService struct {
 AutomaticProjects *v2optical.PerspectiveProjects
 DB v2optical.NegativeTransactions
 Reader v2input.SessionAnalysisObjectReader
 Objects artifactjson.Objects
 Bucket string
 Policy PerspectiveLinesExecutionPolicy
}

// Advance reloads the decision and all physical sources for each pass. Detector
// admission does not settle the replacement branch or publish a perspective task.
func(s ReplacementDetectionWaveService) Advance(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,decision v2optical.NegativeDeclinedDecision,limit int)(ReplacementDetectionWavePass,error){
 var out ReplacementDetectionWavePass
 if err:=ctx.Err();err!=nil{return out,err}
 if s.DB==nil||s.Reader==nil||s.Objects==nil||len(s.Bucket)<3||len(s.Bucket)>63||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||tenant==uuid.Nil||project==uuid.Nil||decision.Route!="base"||limit<1||limit>100{return out,v2input.ErrInvalid}
 _,settlement,err:=v2optical.ReconcileNegativeInvalidation(ctx,s.DB,who,tenant,project,decision.Review,decision.OriginTask,decision.OriginRun,decision.DecisionHash,decision.Route,4096,65536)
 if err!=nil{return out,err};if !settlement.Settled{out.WaitingForSettlement=true;return out,nil}
 prepared,err:=v2optical.LoadNegativeReplacementInputs(ctx,s.DB,s.Reader,s.Bucket,who,tenant,project,decision);if err!=nil{return out,err}
 topology,err:=prepared.Topology();if err!=nil{return out,err};out.Total=len(topology.Nodes)
 rebuilt,retained,err:=prepared.LoadAdoptedReplacementParents(ctx,s.DB,s.Reader);if err!=nil{return out,err};out.Adopted=len(rebuilt)
 mappings,unchanged,err:=prepared.LoadMappedReadySources(ctx,s.DB,s.Reader,rebuilt,retained);if err!=nil{return out,err}
 ready,err:=prepared.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return out,err};out.Ready=len(ready)
 type selected struct{node v2optical.NegativeReplacementNode;documents ReplacementDetectionDocuments;step v2optical.PreparedMappedReplacementStep}
 jobs:=make([]selected,0,len(ready))
 // Validate every selected descriptor and policy before starting object writes.
 for _,step:=range ready{
  auto,err:=step.AutomaticPerspectiveDetection(ctx);if err!=nil{return out,err}
  node:=step.Node();node.Parents=append([]v2optical.NegativeReplacementParent(nil),node.Parents...)
  if !auto{out.Deferred=append(out.Deferred,node);continue}
  plan,err:=CompilePerspectiveReplacementDetectionPlan(ctx,step);if err!=nil{return out,err}
  if _,err=s.Policy.ForPlan(plan.Detector());err!=nil{return out,err}
  snapshot,err:=prepared.ReadOriginalReplacementSnapshot(ctx,s.DB,node.OriginalTask);if err!=nil{return out,err}
  documents:=ReplacementDetectionDocuments{DB:s.DB,Bucket:s.Bucket,Prepared:prepared,Plan:plan,Snapshot:snapshot,Rebuilt:rebuilt,Retained:retained,Mappings:mappings,Unchanged:unchanged}
  jobs=append(jobs,selected{node:node,documents:documents,step:step})
 }
 for i,job:=range jobs{
  if out.Publications>=limit{for _,pending:=range jobs[i:]{out.Pending=append(out.Pending,pending.node)};break}
  recovered,err:=job.documents.PrepareAndActivate(ctx,s.Policy,s.Objects,s.Reader)
  if err!=nil{out.Unconfirmed=append(out.Unconfirmed,job.node);for _,pending:=range jobs[i+1:]{out.Pending=append(out.Pending,pending.node)};return out,err}
  detector:=job.documents.Plan.Detector()
  out.Admissions=append(out.Admissions,ReplacementDetectionAdmission{OriginalTask:job.node.OriginalTask,Run:detector.scope.Run,Task:detector.Task(),RecoveredExisting:recovered})
  if !recovered{out.Publications++}
  if s.AutomaticProjects==nil||out.Publications>=limit{continue}
  err=func()error{
   adopted,err:=job.documents.WasAdopted(ctx);if err!=nil{return err}
   if !adopted{out.Pending=append(out.Pending,job.node);return nil}
   observed,err:=job.documents.ReadDetectedLines(ctx);if err!=nil{return err}
   if observed.Detection.Status=="unsupported"{out.Unsupported=append(out.Unsupported,job.node);return nil}
   plan,err:=job.documents.PublishSegments(ctx,s.Objects);if err!=nil{return err}
   segments,err:=job.documents.VerifyReadySegments(ctx,plan,s.Reader);if err!=nil{return err}
   qualification,err:=s.AutomaticProjects.PrepareAutomaticReplacementQualification(ctx,job.step,segments.Ref(),segments.Bytes());if err!=nil{return err}
   receipt,err:=job.documents.ExecuteAutomaticPerspectiveReplacement(ctx,s.Objects,segments,qualification.Located,qualification.Locks,qualification.MaxPixels,qualification.Capacity);if err!=nil{return err}
   out.AutomaticReceipts=append(out.AutomaticReceipts,receipt)
   if !receipt.RecoveredExisting{out.Publications++}
   return nil
  }()
  if err!=nil{out.Unconfirmed=append(out.Unconfirmed,job.node);for _,pending:=range jobs[i+1:]{out.Pending=append(out.Pending,pending.node)};return out,err}
 }
 return out,ctx.Err()
}
