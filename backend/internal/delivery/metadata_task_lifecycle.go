package delivery

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

type MetadataTaskLifecycle struct {DB ProofTransaction}
func NewMetadataTaskLifecycle(db MetadataTransactionBeginner,tenant uuid.UUID)(MetadataTaskLifecycle,error){
 if db==nil||tenant==uuid.Nil{return MetadataTaskLifecycle{},v2input.ErrInvalid}
 return MetadataTaskLifecycle{DB:MetadataTenantQueries{DB:db,Tenant:tenant}},nil
}
func(s MetadataTaskLifecycle)call(ctx context.Context,scope MetadataProofScope,body []byte,query string,reserved int64)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if s.DB==nil||scope.Tenant==uuid.Nil||scope.Expansion==uuid.Nil||scope.Token==uuid.Nil||len(body)<2||len(body)>1<<20{return false,v2input.ErrInvalid}
 exact,err:=canonical.Bytes(body)
 if err!=nil||!bytes.Equal(exact,body){return false,v2input.ErrInvalid}
 var task struct {ID uuid.UUID `json:"id"`;Operation string `json:"operation"`}
 if json.Unmarshal(exact,&task)!=nil||task.ID!=scope.Task||task.Operation!="metadata.write"{return false,v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(scope.ChildRun)
 if err!=nil{return false,err}
 expected,err:=v2input.TaskID(workflow,"metadata.write",0)
 if err!=nil||expected!=scope.Task{return false,v2input.ErrInvalid}
 args:=[]any{scope.Tenant,scope.Expansion,scope.Token,string(exact)}
 if reserved!=0{args=append(args,reserved)}
 var accepted bool
 if err=s.DB.QueryRow(ctx,query,args...).Scan(&accepted);err!=nil{return false,err}
 if err=ctx.Err();err!=nil{return false,err}
 return accepted,nil
}
func(s MetadataTaskLifecycle)Create(ctx context.Context,scope MetadataProofScope,task []byte,reserved int64)error{
 if reserved<1||reserved>9007199254740991{return v2input.ErrInvalid}
 accepted,err:=s.call(ctx,scope,task,`SELECT photo.create_v30_metadata_task($1,$2,$3,$4,$5)`,reserved)
 if err!=nil{return err};if !accepted{return domain.NewError("DELIVERY_METADATA_STALE",409)};return nil
}
func(s MetadataTaskLifecycle)TransportReady(ctx context.Context,scope MetadataProofScope,task []byte)(bool,error){
 return s.call(ctx,scope,task,`SELECT photo.v30_metadata_transport_ready($1,$2,$3,$4)`,0)
}
func(s MetadataTaskLifecycle)Activated(ctx context.Context,scope MetadataProofScope,task []byte)(bool,error){
 return s.call(ctx,scope,task,`SELECT photo.v30_metadata_task_activated($1,$2,$3,$4)`,0)
}
func(s MetadataTaskLifecycle)Activate(ctx context.Context,scope MetadataProofScope,task []byte)error{
 accepted,err:=s.call(ctx,scope,task,`SELECT photo.activate_v30_metadata_task($1,$2,$3,$4)`,0)
 if err!=nil{return err};if !accepted{return domain.NewError("DELIVERY_METADATA_STALE",409)};return nil
}
