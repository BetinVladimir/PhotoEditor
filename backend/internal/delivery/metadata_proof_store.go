package delivery

import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

type MetadataProofScope struct {
 Tenant,Expansion,Token,ChildRun,Task uuid.UUID
 Encoded,Metadata v2input.Ref
 Binding v2input.SessionVersionBinding
 Policy string
 ApprovedFields []string
}
type SQLMetadataProofStore struct {DB ProofTransaction}

// Pin writes only the immutable proof under the existing expansion lease. It
// neither creates the barrier nor publishes objects or dispatches a task.
func(s SQLMetadataProofStore)Pin(ctx context.Context,scope MetadataProofScope,body []byte,pin string)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil{return domain.NewError("DELIVERY_UNAVAILABLE",503)}
 if scope.Tenant==uuid.Nil||scope.Expansion==uuid.Nil||scope.Token==uuid.Nil{return v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(scope.ChildRun)
 if err!=nil{return err}
 task,err:=v2input.TaskID(workflow,"metadata.write",0)
 if err!=nil||task!=scope.Task{return v2input.ErrInvalid}
 // Own bytes/approval before SQL callbacks can yield.
 body=append([]byte(nil),body...)
 scope.ApprovedFields=append([]string(nil),scope.ApprovedFields...)
 if _,err=RestoreMetadataWriteProof(body,pin,scope.Task,scope.Encoded,scope.Binding,scope.Metadata,scope.Policy,scope.ApprovedFields);err!=nil{return err}
 if err=ctx.Err();err!=nil{return err}
 var accepted bool
 err=s.DB.QueryRow(ctx,`SELECT photo.pin_v30_delivery_metadata_proof($1,$2,$3,$4,$5)`,
 scope.Tenant,scope.Expansion,scope.Token,string(body),pin).Scan(&accepted)
 if err!=nil{return err}
 if err=ctx.Err();err!=nil{return err}
 if !accepted{return domain.NewError("DELIVERY_METADATA_STALE",409)}
 return nil
}


// Load restores a pending immutable proof only under the current barrier lease.
// SQL repeats live producer/artifact authority; Go independently recompiles bytes.
func(s SQLMetadataProofStore)Load(ctx context.Context,scope MetadataProofScope)(MetadataWritePlan,error){
 var zero MetadataWritePlan
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil{return zero,domain.NewError("DELIVERY_UNAVAILABLE",503)}
 if scope.Tenant==uuid.Nil||scope.Expansion==uuid.Nil||scope.Token==uuid.Nil{return zero,v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(scope.ChildRun)
 if err!=nil{return zero,err}
 task,err:=v2input.TaskID(workflow,"metadata.write",0)
 if err!=nil||task!=scope.Task{return zero,v2input.ErrInvalid}
 scope.ApprovedFields=append([]string(nil),scope.ApprovedFields...)
 var body,pin string
 err=s.DB.QueryRow(ctx,`SELECT proof_jcs,proof_sha FROM photo.read_v30_delivery_metadata_proof($1,$2,$3)`,
 scope.Tenant,scope.Expansion,scope.Token).Scan(&body,&pin)
 if errors.Is(err,pgx.ErrNoRows){return zero,domain.NewError("DELIVERY_METADATA_STALE",409)}
 if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 plan,err:=RestoreMetadataWriteProof([]byte(body),pin,scope.Task,scope.Encoded,scope.Binding,scope.Metadata,scope.Policy,scope.ApprovedFields)
 if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 return plan,nil
}
