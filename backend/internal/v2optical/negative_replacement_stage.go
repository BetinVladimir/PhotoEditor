package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func(r negativeReplacementParamsRepository)descriptor(ctx context.Context,published v2input.Ref)([]byte,error){
 snapshot,plan,err:=r.document(ctx);if err!=nil{return nil,err}
 qualified,err:=r.located.Qualification();if err!=nil{return nil,err}
 _,params,source,err:=RestoreOwnedNegativeRecovery(ctx,[]byte(snapshot.RecoveryJCS),snapshot.Owner,qualified);if err!=nil{return nil,err}
 if published!=params{return nil,v2input.ErrInvalid}
 entries,_,err:=NegativeTaskInputEntries(ctx,plan,source,qualified,params);if err!=nil{return nil,err}
 digest,err:=negativeDraftInputDigest(snapshot.Owner.TaskID,entries);if err!=nil{return nil,err}
 if digest!=snapshot.InputDigest{return nil,v2input.ErrInvalid}
 node,err:=v2input.NodeKey("optical.negative",0);if err!=nil{return nil,err}
 task:=v2input.DraftTask{ID:snapshot.Owner.TaskID,NodeKey:node,Operation:"optical.negative",OperationVersion:"1",ResourceClass:"cpu",ParamsArtifactID:params.ArtifactID,ParamsSHA:params.SHA256,InputDigest:digest,ExecutionProfile:plan.Profile}
 raw,err:=canonical.Marshal(struct{
  SchemaVersion int `json:"schema_version"`
  Task v2input.DraftTask `json:"task"`
  Entries []v2input.Entry `json:"entries"`
  ReservedBytes int64 `json:"reserved_bytes"`
 }{1,task,entries,snapshot.ReservedBytes})
 if err!=nil||len(raw)>1<<20{return nil,v2input.ErrInvalid}
 return raw,ctx.Err()
}

// StageNegativeReplacementTask creates only a blocked shell and exact output
// reservation. Dedicated transport, closure registration and seal remain gates.
func(p PreparedNegativeReplacementInputs)StageNegativeReplacementTask(ctx context.Context,db NegativeTransactions,bucket string,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft NegativeReplacementDraft,snapshot uuid.UUID,located LocatedNegativeQualification,locks v2qualification.DeploymentLocks,capacity NegativeResourceEstimate,published v2input.Ref)error{
 if err:=ctx.Err();err!=nil{return err}
 if db==nil||bucket!=p.bucket||len(bucket)<3||len(bucket)>63{return v2input.ErrInvalid}
 r:=negativeReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:snapshot,located:located,locks:locks,capacity:capacity,rebuilt:append([]VerifiedAdoptedParentArrays(nil),rebuilt...),retained:append([]VerifiedAdoptedParentArrays(nil),retained...),mappings:append([]VerifiedNegativeAffectedSources(nil),mappings...),unchanged:append([]VerifiedNegativeUnchangedArrays(nil),unchanged...)}
 descriptor,err:=r.descriptor(ctx,published);if err!=nil{return err}
 saved,plan,err:=r.document(ctx);if err!=nil{return err}
 scope:=artifactjson.Scope{Tenant:p.tenant,Run:saved.Owner.RunID,Owner:saved.Owner.TaskID,Purpose:"task_params"}
 proof,err:=r.proof(ctx,scope,published.SHA256,int64(len(plan.Params)));if err!=nil{return err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err=r.authorize(ctx,tx,scope);err!=nil{return err}
 if err=r.lock(ctx,tx);err!=nil{return err}
 var staged bool
 if err=tx.QueryRow(ctx,"SELECT photo.stage_v29_negative_replacement_task($1,$2,$3,$4::jsonb)",p.tenant,saved.Owner.RunID,string(descriptor),proof).Scan(&staged);err!=nil{return err}
 if !staged{return v2input.ErrExpired}
 if err:=ctx.Err();err!=nil{return err}
 return tx.Commit(ctx)
}
