package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// AdmitNegativeReplacementGraph registers exact transport, then retains the
// complete physical/intent/mapping/snapshot/qualification proof in the atomic
// graph seal and queued receipt transaction. Prepared proofs must originate from
// the original-version object readers. Commit errors remain ambiguous: recover
// the persisted receipt; never manufacture a new replacement identity.
func(p PreparedNegativeReplacementInputs)AdmitNegativeReplacementGraph(ctx context.Context,db NegativeTransactions,bucket string,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft NegativeReplacementDraft,snapshot uuid.UUID,located LocatedNegativeQualification,locks v2qualification.DeploymentLocks,capacity NegativeResourceEstimate,published v2input.Ref)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||bucket!=p.bucket||len(bucket)<3||len(bucket)>63{return zero,v2input.ErrInvalid}
 graph,err:=p.RegisterNegativeReplacementInputs(ctx,db,bucket,rebuilt,retained,mappings,unchanged,draft,snapshot,located,locks,capacity,published);if err!=nil{return zero,err}
 repository:=negativeReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:snapshot,located:located,locks:locks,capacity:capacity,rebuilt:append([]VerifiedAdoptedParentArrays(nil),rebuilt...),retained:append([]VerifiedAdoptedParentArrays(nil),retained...),mappings:append([]VerifiedNegativeAffectedSources(nil),mappings...),unchanged:append([]VerifiedNegativeUnchangedArrays(nil),unchanged...)}
 descriptor,err:=repository.descriptor(ctx,published);if err!=nil{return zero,err}
 saved,plan,err:=repository.document(ctx);if err!=nil{return zero,err}
 scope:=artifactjson.Scope{Tenant:p.tenant,Run:saved.Owner.RunID,Owner:saved.Owner.TaskID,Purpose:"task_params"}
 proof,err:=repository.proof(ctx,scope,published.SHA256,int64(len(plan.Params)));if err!=nil{return zero,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err=repository.authorize(ctx,tx,scope);err!=nil{return zero,err}
 if err=repository.lock(ctx,tx);err!=nil{return zero,err}
 var admitted bool
 if err=tx.QueryRow(ctx,"SELECT photo.seal_v29_negative_replacement_graph($1,$2,$3,$4,$5::jsonb)",p.tenant,saved.Owner.RunID,saved.Owner.TaskID,string(descriptor),proof).Scan(&admitted);err!=nil{return zero,err}
 if !admitted{return zero,v2input.ErrExpired}
 if err=ctx.Err();err!=nil{return zero,err}
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 return graph,nil
}
