package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// ReadPublishedNegativeReplacementTransport reads only existing blocked-task
// transport and a live output reservation. No object IO, staging, quota repair
// or registration is initiated by Go. SQL refuses missing task/reservation
// before invoking its exact replay validators.
func(p PreparedNegativeReplacementInputs)ReadPublishedNegativeReplacementTransport(ctx context.Context,db NegativeTransactions,bucket string,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft NegativeReplacementDraft,snapshot uuid.UUID,located LocatedNegativeQualification,locks v2qualification.DeploymentLocks,capacity NegativeResourceEstimate,published v2input.Ref)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||bucket!=p.bucket||len(bucket)<3||len(bucket)>63{return zero,v2input.ErrInvalid}
 r:=negativeReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:snapshot,located:located,locks:locks,capacity:capacity,rebuilt:append([]VerifiedAdoptedParentArrays(nil),rebuilt...),retained:append([]VerifiedAdoptedParentArrays(nil),retained...),mappings:append([]VerifiedNegativeAffectedSources(nil),mappings...),unchanged:append([]VerifiedNegativeUnchangedArrays(nil),unchanged...)}
 descriptor,err:=r.descriptor(ctx,published);if err!=nil{return zero,err}
 saved,plan,err:=r.document(ctx);if err!=nil{return zero,err}
 scope:=artifactjson.Scope{Tenant:p.tenant,Run:saved.Owner.RunID,Owner:saved.Owner.TaskID,Purpose:"task_params"}
 proof,err:=r.proof(ctx,scope,published.SHA256,int64(len(plan.Params)));if err!=nil{return zero,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err=r.authorize(ctx,tx,scope);err!=nil{return zero,err}
 if err=r.lock(ctx,tx);err!=nil{return zero,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_replacement_published_transport($1,$2,$3,$4,$5::jsonb)",p.tenant,saved.Owner.RunID,saved.Owner.TaskID,string(descriptor),proof).Scan(&raw);err!=nil{return zero,err}
 graph,err:=restoreNegativePublishedTransport(ctx,raw,func(ctx context.Context)([]byte,error){return r.descriptor(ctx,published)});if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 return graph,nil
}
