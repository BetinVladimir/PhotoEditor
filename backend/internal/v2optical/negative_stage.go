package v2optical

import (
 "context"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// StageNegativeTask saves only a blocked shell and exact output reservation.
// Current source/qualification versions remain locked through staging commit.
// Transport publication, input registration and graph admission are separate.
func StageNegativeTask(ctx context.Context,db NegativeTransactions,bucket string,who auth.Identity,tenant uuid.UUID,draft NegativeDraft,located LocatedNegativeQualification,published v2input.Ref)error{
 if err:=ctx.Err();err!=nil{return err}
 qualified,err:=located.Qualification();if err!=nil{return err}
 descriptor,err:=draft.Descriptor(ctx,qualified,published);if err!=nil{return err}
 snapshot,err:=draft.Snapshot();if err!=nil{return err}
 plan,_,source,err:=draft.RestorePlan(ctx,qualified);if err!=nil{return err}
 repository:=negativeParamsRepository{db:db,bucket:bucket,tenant:tenant,who:who,plan:draft,located:located}
 scope:=artifactjson.Scope{Tenant:tenant,Run:snapshot.Owner.RunID,Owner:snapshot.Owner.TaskID,Purpose:"task_params"}
 proof,err:=repository.proof(ctx,scope,snapshot.ParamsSHA,int64(len(plan.Params)));if err!=nil{return err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if _,_,err:=LockNegativeAdmission(ctx,tx,who,tenant,snapshot.Owner.ProjectID,snapshot.Owner.SnapshotID,plan,source,located,published);err!=nil{return err}
 var staged bool
 if err:=tx.QueryRow(ctx,"SELECT photo.stage_v29_negative_task($1,$2,$3,$4::jsonb)",tenant,snapshot.Owner.RunID,string(descriptor),proof).Scan(&staged);err!=nil{return err}
 if !staged{return v2input.ErrExpired}
 if err:=ctx.Err();err!=nil{return err}
 return tx.Commit(ctx)
}
