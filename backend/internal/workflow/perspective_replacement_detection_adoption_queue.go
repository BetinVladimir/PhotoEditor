package workflow
import (
 "bytes"
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
// Discovery metadata only; current source authority is reloaded for each job.
type ReplacementDetectionAdoptionCandidate struct{
 Tenant,Run,Task,Snapshot,Result uuid.UUID
 Descriptor []byte
 Attempt int
 Fence int64
 Root v2input.Ref
}
type ReplacementDetectionAdoptionQueue struct{DB interface{QueryRow(context.Context,string,...any)pgx.Row}}
func(q ReplacementDetectionAdoptionQueue) Next(ctx context.Context,after uuid.UUID)(*ReplacementDetectionAdoptionCandidate,error){
 if err:=ctx.Err();err!=nil{return nil,err};if q.DB==nil{return nil,v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 var out ReplacementDetectionAdoptionCandidate;var descriptor string
 err:=q.DB.QueryRow(ctx,"SELECT tenant_id,run_id,task_id,snapshot_id,descriptor_jcs,attempt,fence,result_id,root_id,root_sha FROM photo.next_v29_replacement_detector_adoption($1)",after).Scan(&out.Tenant,&out.Run,&out.Task,&out.Snapshot,&descriptor,&out.Attempt,&out.Fence,&out.Result,&out.Root.ArtifactID,&out.Root.SHA256)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 d,err:=decodeReplacementDetectionDescriptor([]byte(descriptor));if err!=nil{return nil,err}
 if d.Tenant!=out.Tenant||d.Run!=out.Run||d.Task!=out.Task||out.Snapshot==uuid.Nil||out.Result==uuid.Nil||out.Root.ArtifactID==uuid.Nil||domainDigestInvalid(out.Root.SHA256)||out.Attempt<1||out.Attempt>6||out.Fence<1||out.Fence>9007199254740991||bytes.Compare(out.Run[:],after[:])<=0{return nil,v2input.ErrInvalid}
 out.Descriptor=[]byte(descriptor)
 if err=ctx.Err();err!=nil{return nil,err};return &out,nil
}
