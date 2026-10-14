package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// AdmitNegativeGraph verifies original array bytes outside SQL, registers the
// complete closure, and repeats exact physical locators plus live qualification
// inside the atomic graph seal/queued-receipt transaction. A commit error is
// ambiguous; callers must recover the saved receipt rather than blindly resubmit.
func AdmitNegativeGraph(ctx context.Context, db NegativeTransactions, objects v2input.SessionAnalysisObjectReader, bucket string, who auth.Identity, tenant uuid.UUID, draft NegativeDraft, located LocatedNegativeQualification, published v2input.Ref) (v2input.GraphTask,error) {
 var zero v2input.GraphTask
 if err:=ctx.Err();err!=nil { return zero,err }
 if db==nil || objects==nil || bucket=="" || len(bucket)>255 { return zero,v2input.ErrInvalid }
 snapshot,err:=draft.Snapshot();if err!=nil { return zero,err }
 if snapshot.Owner.TenantID!=tenant || snapshot.Owner.ActorID!=who.UserID || snapshot.Owner.AuthSessionID!=who.SessionID { return zero,v2input.ErrInvalid }
 qualified,err:=located.Qualification();if err!=nil { return zero,err }
 plan,paramsRef,source,err:=draft.RestorePlan(ctx,qualified);if err!=nil { return zero,err }
 if published!=paramsRef { return zero,v2input.ErrInvalid }
 params,err:=ParseNegativeParams(plan.Params);if err!=nil { return zero,err }
 physical,err:=readNegativeSourceProof(ctx,SQLNegativeSourceArrayCatalog{DB:db,Bucket:bucket},objects,who,tenant,snapshot.Owner.ProjectID,snapshot.Owner.SnapshotID,params,source);if err!=nil { return zero,err }
 graph,err:=RegisterNegativeInputs(ctx,db,bucket,who,tenant,draft,located,published);if err!=nil { return zero,err }
 descriptor,err:=draft.Descriptor(ctx,qualified,published);if err!=nil { return zero,err }
 repository:=negativeParamsRepository{db:db,bucket:bucket,tenant:tenant,who:who,plan:draft,located:located}
 scope:=artifactjson.Scope{Tenant:tenant,Run:snapshot.Owner.RunID,Owner:snapshot.Owner.TaskID,Purpose:"task_params"}
 proof,err:=repository.proof(ctx,scope,snapshot.ParamsSHA,int64(len(plan.Params)));if err!=nil { return zero,err }
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil { return zero,err };if tx==nil { return zero,v2input.ErrInvalid }
 defer tx.Rollback(context.WithoutCancel(ctx))
 if _,_,err:=LockNegativeAdmission(ctx,tx,who,tenant,snapshot.Owner.ProjectID,snapshot.Owner.SnapshotID,plan,source,located,published);err!=nil { return zero,err }
 if err:=lockNegativeSourceProof(ctx,tx,bucket,who,tenant,snapshot.Owner.ProjectID,snapshot.Owner.SnapshotID,params,source,physical);err!=nil { return zero,err }
 var admitted bool
 if err:=tx.QueryRow(ctx,"SELECT photo.seal_v29_negative_graph($1,$2,$3,$4,$5::jsonb)",tenant,snapshot.Owner.RunID,snapshot.Owner.TaskID,string(descriptor),proof).Scan(&admitted);err!=nil { return zero,err }
 if !admitted { return zero,v2input.ErrExpired }
 if err:=ctx.Err();err!=nil { return zero,err }
 if err:=tx.Commit(ctx);err!=nil { return zero,err }
 return graph,nil
}
