package workflow
import (
 "context"
 "bytes"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)
// Compilation only. Ordinary accepted-version detector admission must not be
// used for replacement source; dedicated SQL authority is still required.
type PerspectiveReplacementDetectionPlan struct {
 detector PerspectiveLinesPlan
 descriptor string
}
func(p PerspectiveReplacementDetectionPlan) Detector() PerspectiveLinesPlan{return p.detector}
func(p PerspectiveReplacementDetectionPlan) Descriptor() json.RawMessage{return []byte(p.descriptor)}
func CompilePerspectiveReplacementDetectionPlan(ctx context.Context,step v2optical.PreparedMappedReplacementStep)(PerspectiveReplacementDetectionPlan,error){
 var zero PerspectiveReplacementDetectionPlan
 source,err:=step.PerspectiveDetectionSource(ctx);if err!=nil{return zero,err}
 node:=step.Node()
 run:=uuid.NewSHA1(node.Run,[]byte("V29.PerspectiveReplacementDetection/v1"))
 image,err:=review.ParseWorkingImage(source.WorkingImage);if err!=nil{return zero,err}
 plan,err:=CompilePerspectiveLinesPlan(PerspectiveLinesScope{Tenant:source.Tenant,Project:source.Project,Run:run,Base:source.Base},source.WorkingImage,image.Color.ICCSHA256);if err!=nil{return zero,err}
 if source.Principal.UserID==uuid.Nil||source.Principal.SessionID==uuid.Nil||source.Review==uuid.Nil||domainDigestInvalid(source.DecisionHash){return zero,v2input.ErrInvalid}
 descriptor,err:=canonical.Marshal(perspectiveReplacementDetectionDescriptor{
 SchemaVersion:1,Kind:"v29_perspective_replacement_detection_v1",Tenant:source.Tenant,Project:source.Project,Review:source.Review,DecisionHash:source.DecisionHash,
 Actor:source.Principal.UserID,Session:source.Principal.SessionID,Replacement:node,Run:run,Task:plan.Task(),Base:source.Base,
 ParamsJCS:string(plan.Params()),SourceJCS:string(source.WorkingImage),ProfileJCS:string(plan.Profile()),
 });if err!=nil{return zero,err}
 if _,err=decodeReplacementDetectionDescriptor(descriptor);err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 return PerspectiveReplacementDetectionPlan{detector:plan,descriptor:string(descriptor)},nil
}

type perspectiveReplacementDetectionDescriptor struct {
 SchemaVersion int `json:"schema_version"`
 Kind string `json:"kind"`
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Review uuid.UUID `json:"review_id"`
 DecisionHash string `json:"decision_hash"`
 Actor uuid.UUID `json:"actor_id"`
 Session uuid.UUID `json:"auth_session_id"`
 Replacement v2optical.NegativeReplacementNode `json:"replacement"`
 Run uuid.UUID `json:"run_id"`
 Task uuid.UUID `json:"task_id"`
 Base v2input.SessionVersionBinding `json:"base"`
 ParamsJCS string `json:"params_jcs"`
 SourceJCS string `json:"source_jcs"`
 ProfileJCS string `json:"profile_jcs"`
}
func decodeReplacementDetectionDescriptor(raw []byte)(perspectiveReplacementDetectionDescriptor,error){
 var value perspectiveReplacementDetectionDescriptor
 if len(raw)<2||len(raw)>3<<20{return value,v2input.ErrInvalid}
 if json.Unmarshal(raw,&value)!=nil{return value,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(value)
 if err!=nil||!bytes.Equal(raw,exact)||value.SchemaVersion!=1||value.Kind!="v29_perspective_replacement_detection_v1"{return value,v2input.ErrInvalid}
 if value.Tenant==uuid.Nil||value.Project==uuid.Nil||value.Review==uuid.Nil||value.Actor==uuid.Nil||value.Session==uuid.Nil||domainDigestInvalid(value.DecisionHash){return value,v2input.ErrInvalid}
 node:=value.Replacement
 if node.OriginalTask==uuid.Nil||node.OriginalRun==uuid.Nil||node.Run==uuid.Nil||node.Task==uuid.Nil{return value,v2input.ErrInvalid}
 namespace:=uuid.NewSHA1(value.Tenant,[]byte("V29.NegativeReplacement/v2:"+value.Project.String()+":"+value.Review.String()+":"+value.DecisionHash))
 expectedTask:=uuid.NewSHA1(namespace,[]byte("task:"+node.OriginalTask.String()))
 expectedRun:=uuid.NewSHA1(namespace,[]byte("run:"+node.OriginalRun.String()+":"+node.OriginalTask.String()))
 if node.Task!=expectedTask||node.Run!=expectedRun{return value,v2input.ErrInvalid}
 parents:=make(map[uuid.UUID]bool,len(node.Parents))
 for _,parent:=range node.Parents{
  if parent.Original==uuid.Nil||parents[parent.Original]||parent.Original==node.OriginalTask{return value,v2input.ErrInvalid}
  parents[parent.Original]=true
  switch parent.Kind{
  case "replacement":if parent.Task!=uuid.NewSHA1(namespace,[]byte("task:"+parent.Original.String())){return value,v2input.ErrInvalid}
  case "selected_base":if parent.Task!=uuid.Nil{return value,v2input.ErrInvalid}
  case "retained":if parent.Task!=parent.Original{return value,v2input.ErrInvalid}
  default:return value,v2input.ErrInvalid
  }
 }
 run:=uuid.NewSHA1(node.Run,[]byte("V29.PerspectiveReplacementDetection/v1"))
 image,err:=review.ParseWorkingImage([]byte(value.SourceJCS));if err!=nil{return value,v2input.ErrInvalid}
 plan,err:=CompilePerspectiveLinesPlan(PerspectiveLinesScope{Tenant:value.Tenant,Project:value.Project,Run:run,Base:value.Base},[]byte(value.SourceJCS),image.Color.ICCSHA256)
 if err!=nil||value.Run!=run||value.Task!=plan.Task()||value.ParamsJCS!=string(plan.Params())||value.ProfileJCS!=string(plan.Profile()){return value,v2input.ErrInvalid}
 return value,nil
}
// Saved bytes are never authority: restore recompiles from freshly prepared
// mapped source proofs and compares the entire closed descriptor.
func RestorePerspectiveReplacementDetectionPlan(ctx context.Context,step v2optical.PreparedMappedReplacementStep,raw []byte)(PerspectiveReplacementDetectionPlan,error){
 var zero PerspectiveReplacementDetectionPlan
 if err:=ctx.Err();err!=nil{return zero,err}
 if _,err:=decodeReplacementDetectionDescriptor(raw);err!=nil{return zero,err}
 fresh,err:=CompilePerspectiveReplacementDetectionPlan(ctx,step);if err!=nil{return zero,err}
 if !bytes.Equal(raw,fresh.Descriptor()){return zero,v2input.ErrExpired}
 return fresh,ctx.Err()
}
