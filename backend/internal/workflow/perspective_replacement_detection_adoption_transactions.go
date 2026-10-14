package workflow

import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)

// adoptionTransaction owns the final transaction boundary for a configured
// scheduler bridge. Source locks are acquired by the callback and retained until
// commit. No object IO or Python replay may run inside this bounded transaction.
func(r ReplacementDetectionDocuments) adoptionTransaction(ctx context.Context,apply func(context.Context,pgx.Tx)error)error{
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return err}
 if apply==nil{return v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=r.DB.Begin(ctx);if err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_scheduler"{return v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",scope.Tenant.String());err!=nil{return err}
 if err=apply(ctx,tx);err!=nil{return err}
 if err=ctx.Err();err!=nil{return err};return tx.Commit(ctx)
}

// ReadAdoptionEvidence is a current snapshot for independent replay outside SQL.
// A later commit must reload complete physical authority; this snapshot grants
// no durable permission and cannot be reused as proof of current source state.
func(r ReplacementDetectionDocuments) ReadAdoptionEvidence(ctx context.Context,attempt int,fence int64,result uuid.UUID)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if attempt<1||attempt>6||fence<1||fence>9007199254740991||result==uuid.Nil{return nil,v2input.ErrInvalid}
 var raw []byte
 err:=r.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  var err error
  raw,err=r.AdoptionEvidence(ctx,tx,attempt,fence,result);return err
 });if err!=nil{return nil,err};return append([]byte(nil),raw...),nil
}

// CommitReplayedAdoption accepts the trusted scheduler's pinned replay packet
// and commits only after independently refreshing all physical authority.
// A lost successful response must be recovered from the immutable SQL receipt.
func(r ReplacementDetectionDocuments) CommitReplayedAdoption(ctx context.Context,attempt int,fence int64,result uuid.UUID,inventory []byte,digest string,evidence,root []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if attempt<1||attempt>6||fence<1||fence>9007199254740991||result==uuid.Nil{return v2input.ErrInvalid}
 if len(inventory)<2||len(inventory)>8<<20||len(evidence)<2||len(evidence)>262144||len(root)<2||len(root)>1048576||domainDigestInvalid(digest){return v2input.ErrInvalid}
 // Own all packet bytes before transaction callbacks or external mutation.
 inventory=append([]byte(nil),inventory...);evidence=append([]byte(nil),evidence...);root=append([]byte(nil),root...)
 return r.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  return r.CommitAdoption(ctx,tx,attempt,fence,result,inventory,digest,evidence,root)
 })
}
