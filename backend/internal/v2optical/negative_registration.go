package v2optical

import (
 "context"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// RegisterNegativeInputs reopens the exact published transport, then repeats
// live owner/source/qualification and full descriptor authority while registering
// all entry and transport rows/sealing task inputs in one transaction. The graph
// remains draft/unsealed and no queued acknowledgement is produced here.
func RegisterNegativeInputs(ctx context.Context,db NegativeTransactions,bucket string,who auth.Identity,tenant uuid.UUID,draft NegativeDraft,located LocatedNegativeQualification,published v2input.Ref)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 graph,err:=ReadPublishedNegativeTransport(ctx,db,bucket,who,tenant,draft,located,published);if err!=nil{return zero,err}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 descriptor,err:=draft.Descriptor(ctx,qualified,published);if err!=nil{return zero,err}
 transport,err:=canonical.Marshal(struct{
  Manifest v2input.Ref `json:"manifest"`
  Chunks []v2input.Ref `json:"chunks"`
  Count int `json:"count"`
  SHA string `json:"sha256"`
 }{graph.Built.Manifest,graph.Built.Chunks,graph.Built.Count,graph.Built.SHA256});if err!=nil{return zero,err}
 manifest,err:=canonical.Marshal(v2input.Manifest{SchemaVersion:1,TaskID:graph.ID,EntryCount:graph.Built.Count,EntriesSHA:graph.Built.SHA256,Chunks:graph.Built.Chunks});if err!=nil{return zero,err}
 snapshot,err:=draft.Snapshot();if err!=nil{return zero,err}
 plan,_,source,err:=draft.RestorePlan(ctx,qualified);if err!=nil{return zero,err}
 repository:=negativeParamsRepository{db:db,bucket:bucket,tenant:tenant,who:who,plan:draft,located:located}
 scope:=artifactjson.Scope{Tenant:tenant,Run:snapshot.Owner.RunID,Owner:snapshot.Owner.TaskID,Purpose:"task_params"}
 proof,err:=repository.proof(ctx,scope,snapshot.ParamsSHA,int64(len(plan.Params)));if err!=nil{return zero,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if _,_,err:=LockNegativeAdmission(ctx,tx,who,tenant,snapshot.Owner.ProjectID,snapshot.Owner.SnapshotID,plan,source,located,published);err!=nil{return zero,err}
 var registered bool
 if err:=tx.QueryRow(ctx,"SELECT photo.register_v29_negative_transport($1,$2,$3,$4::jsonb,$5::jsonb,$6)",tenant,snapshot.Owner.RunID,string(descriptor),proof,string(transport),string(manifest)).Scan(&registered);err!=nil{return zero,err}
 if !registered{return zero,v2input.ErrExpired}
 if err:=tx.Commit(ctx);err!=nil{return zero,err}
 return graph,nil
}
