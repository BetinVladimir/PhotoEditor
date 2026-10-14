package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func(r multiReplacementParamsRepository)descriptor(ctx context.Context,published v2input.Ref)([]byte,error){
 if _,_,err:=r.document(ctx);err!=nil{return nil,err}
 qualified,err:=r.located.Qualification();if err!=nil{return nil,err}
 return r.draft.Descriptor(ctx,qualified,published)
}

// StageMultiReplacementTask creates only a blocked shell and exact output
// reservation. Dedicated transport, closure registration and seal remain gates.
func(p PreparedNegativeReplacementInputs)StageMultiReplacementTask(ctx context.Context,db NegativeTransactions,bucket string,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft MultiReplacementDraft,snapshot uuid.UUID,located LocatedMultiQualification,locks v2qualification.DeploymentLocks,capacity MultiResourceEstimate,published v2input.Ref)error{
 if err:=ctx.Err();err!=nil{return err}
 if db==nil||bucket!=p.bucket||len(bucket)<3||len(bucket)>63{return v2input.ErrInvalid}
 r:=multiReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:snapshot,located:located,locks:locks,capacity:capacity,rebuilt:append([]VerifiedAdoptedParentArrays(nil),rebuilt...),retained:append([]VerifiedAdoptedParentArrays(nil),retained...),mappings:append([]VerifiedNegativeAffectedSources(nil),mappings...),unchanged:append([]VerifiedNegativeUnchangedArrays(nil),unchanged...)}
 descriptor,err:=r.descriptor(ctx,published);if err!=nil{return err}
 saved,plan,err:=r.document(ctx);if err!=nil{return err}
 scope:=artifactjson.Scope{Tenant:p.tenant,Run:saved.Identity.Run,Owner:saved.Identity.Task,Purpose:"task_params"}
 proof,err:=r.proof(ctx,scope,published.SHA256,int64(len(plan.Params)));if err!=nil{return err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err=r.authorize(ctx,tx,scope);err!=nil{return err}
 if err=r.lock(ctx,tx);err!=nil{return err}
 var staged bool
 if err=tx.QueryRow(ctx,"SELECT photo.stage_v29_multi_replacement_task($1,$2,$3,$4::jsonb)",p.tenant,saved.Identity.Run,string(descriptor),proof).Scan(&staged);err!=nil{return err}
 if !staged{return v2input.ErrExpired}
 if err:=ctx.Err();err!=nil{return err}
 return tx.Commit(ctx)
}
