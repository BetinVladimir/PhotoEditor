package v2optical

import (
 "context"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// ReadPublishedNegativeTransport restores committed metadata without object IO,
// publication, quota repair or registration. SQL requires an existing blocked
// task/live reservation before repeating all stored descriptor authority.
func ReadPublishedNegativeTransport(ctx context.Context,db NegativeTransactions,bucket string,who auth.Identity,tenant uuid.UUID,draft NegativeDraft,located LocatedNegativeQualification,published v2input.Ref)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 if err:=ctx.Err();err!=nil{return zero,err}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 descriptor,err:=draft.Descriptor(ctx,qualified,published);if err!=nil{return zero,err}
 snapshot,err:=draft.Snapshot();if err!=nil{return zero,err}
 plan,_,source,err:=draft.RestorePlan(ctx,qualified);if err!=nil{return zero,err}
 repository:=negativeParamsRepository{db:db,bucket:bucket,tenant:tenant,who:who,plan:draft,located:located}
 scope:=artifactjson.Scope{Tenant:tenant,Run:snapshot.Owner.RunID,Owner:snapshot.Owner.TaskID,Purpose:"task_params"}
 proof,err:=repository.proof(ctx,scope,snapshot.ParamsSHA,int64(len(plan.Params)));if err!=nil{return zero,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err:=repository.authorize(ctx,tx,scope);err!=nil{return zero,err}
 var raw []byte
 if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_published_transport($1,$2,$3,$4,$5::jsonb)",tenant,snapshot.Owner.RunID,snapshot.Owner.TaskID,string(descriptor),proof).Scan(&raw);err!=nil{return zero,err}
 graph,err:=draft.RestorePublishedTransport(ctx,raw,qualified,published);if err!=nil{return zero,err}
 if _,_,err:=LockNegativeAdmission(ctx,tx,who,tenant,snapshot.Owner.ProjectID,snapshot.Owner.SnapshotID,plan,source,located,published);err!=nil{return zero,err}
 if err:=tx.Commit(ctx);err!=nil{return zero,err}
 return graph,nil
}
