package delivery

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// BindPublishedInputs reconstructs transport from the owned metadata inventory.
// It never invokes the supplied graph cursor and performs no object IO.
func(p MetadataWritePlan)BindPublishedInputs(ctx context.Context,scope MetadataProofScope,graph v2input.GraphTask)(v2input.GraphTask,error){
 closure,err:=p.InputClosure(scope.ChildRun)
 if err!=nil{return v2input.GraphTask{},err}
 workflow,_:=v2input.WorkflowID(scope.ChildRun)
 task,err:=v2input.TaskID(workflow,"metadata.write",0)
 if err!=nil||task!=scope.Task{return v2input.GraphTask{},v2input.ErrInvalid}
 return bindPublishedClosure(ctx,task,closure,graph)
}

// RegisterInputsInTransaction requires caller-owned scheduler TX/tenant context.
// Every error must roll back. Physical publication occurs before this call.
func(p MetadataWritePlan)RegisterInputsInTransaction(ctx context.Context,tx pgx.Tx,scope MetadataProofScope,draft v2input.DraftTask,graph v2input.GraphTask,reserved int64)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil{return v2input.ErrInvalid}
 bound,err:=p.BindPublishedInputs(ctx,scope,graph)
 if err!=nil{return err}
 closure,err:=p.InputClosure(scope.ChildRun)
 if err!=nil{return err}
 if draft.ID!=scope.Task||draft.NodeKey!="metadata.write:0"||draft.Operation!="metadata.write"||
 draft.OperationVersion!="1"||draft.ResourceClass!="cpu"||draft.ParamsArtifactID!=p.params.ArtifactID||
 draft.ParamsSHA!=p.params.SHA256||draft.InputDigest!=closure.InputDigest{return v2input.ErrInvalid}
 taskRaw,err:=canonical.Marshal(draft)
 if err!=nil{return err}
 lifecycle:=MetadataTaskLifecycle{DB:tx}
 if activated,err:=lifecycle.Activated(ctx,scope,taskRaw);err!=nil||activated{return err}
 if ready,err:=lifecycle.TransportReady(ctx,scope,taskRaw);err!=nil||ready{return err}
 if err=lifecycle.Create(ctx,scope,taskRaw,reserved);err!=nil{return err}
 if err=v2input.RegisterInTransaction(ctx,tx,scope.Tenant,scope.ChildRun,scope.Task,bound.Built,closure.Open,closure.Direct);err!=nil{return err}
 ready,err:=lifecycle.TransportReady(ctx,scope,taskRaw)
 if err!=nil{return err};if !ready{return v2input.ErrInvalid}
 return ctx.Err()
}
