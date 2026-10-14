package workflow
import (
 "context"
 "bytes"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// PreparePublication compiles the exact document/task/input packet. No object
// writes or SQL authority: dedicated repository must repeat preparation locks.
func(p PerspectiveReplacementDetectionPlan) PreparePublication(ctx context.Context)(artifactjson.Scope,[]byte,PreparedTask,[]v2input.Entry,error){
 var scope artifactjson.Scope;var task PreparedTask
 if err:=ctx.Err();err!=nil{return scope,nil,task,nil,err}
 descriptor,err:=decodeReplacementDetectionDescriptor([]byte(p.descriptor));if err!=nil{return scope,nil,task,nil,err}
 detector:=p.detector
 if detector.scope.Tenant!=descriptor.Tenant||detector.scope.Project!=descriptor.Project||detector.scope.Run!=descriptor.Run||detector.scope.Base!=descriptor.Base||detector.task!=descriptor.Task||string(detector.image)!=descriptor.SourceJCS||string(detector.params)!=descriptor.ParamsJCS||string(detector.profile)!=descriptor.ProfileJCS{return scope,nil,task,nil,v2input.ErrInvalid}
 params:=review.ArtifactRef{ID:uuid.NewSHA1(detector.task,[]byte("params")),SHA256:renderDerivativeBytesSHA(detector.params)}
 task,err=PreparePerspectiveLinesTask(detector,params);if err!=nil{return scope,nil,PreparedTask{},nil,err}
 entries,err:=detector.Entries(params);if err!=nil{return scope,nil,PreparedTask{},nil,err}
 scope=artifactjson.Scope{Tenant:descriptor.Tenant,Run:descriptor.Run,Owner:descriptor.Task,Purpose:"task_params"}
 if err=ctx.Err();err!=nil{return artifactjson.Scope{},nil,PreparedTask{},nil,err}
 return scope,bytes.Clone(detector.params),task,entries,nil
}
