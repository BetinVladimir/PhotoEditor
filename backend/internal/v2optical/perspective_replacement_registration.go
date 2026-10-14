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

// RegisterManualPerspectiveReplacementInputs registers the exact existing transport and
// complete closure in one transaction. It leaves the run draft/unsealed; no
// queued receipt is produced and absent tasks/reservations are never repaired.
func(p PreparedNegativeReplacementInputs)RegisterManualPerspectiveReplacementInputs(ctx context.Context,db NegativeTransactions,bucket string,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft PerspectiveReplacementDraft,snapshot uuid.UUID,located LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity PerspectiveResourceEstimate,published v2input.Ref)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 graph,err:=p.ReadPublishedManualPerspectiveReplacementTransport(ctx,db,bucket,rebuilt,retained,mappings,unchanged,draft,snapshot,located,locks,maxPixels,capacity,published);if err!=nil{return zero,err}
 repository:=manualPerspectiveReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:snapshot,located:located,locks:locks,maxPixels:maxPixels,capacity:capacity,rebuilt:append([]VerifiedAdoptedParentArrays(nil),rebuilt...),retained:append([]VerifiedAdoptedParentArrays(nil),retained...),mappings:append([]VerifiedNegativeAffectedSources(nil),mappings...),unchanged:append([]VerifiedNegativeUnchangedArrays(nil),unchanged...)}
 descriptor,err:=repository.descriptor(ctx,published);if err!=nil{return zero,err}
 transport,err:=canonical.Marshal(struct{
  Manifest v2input.Ref `json:"manifest"`
  Chunks []v2input.Ref `json:"chunks"`
  Count int `json:"count"`
  SHA string `json:"sha256"`
 }{graph.Built.Manifest,graph.Built.Chunks,graph.Built.Count,graph.Built.SHA256});if err!=nil{return zero,err}
 manifest,err:=canonical.Marshal(v2input.Manifest{SchemaVersion:1,TaskID:graph.ID,EntryCount:graph.Built.Count,EntriesSHA:graph.Built.SHA256,Chunks:graph.Built.Chunks});if err!=nil{return zero,err}
 saved,plan,err:=repository.document(ctx);if err!=nil{return zero,err}
 scope:=artifactjson.Scope{Tenant:p.tenant,Run:saved.Identity.Run,Owner:saved.Identity.Task,Purpose:"task_params"}
 proof,err:=repository.proof(ctx,scope,published.SHA256,int64(len(plan.Params)));if err!=nil{return zero,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err=repository.authorize(ctx,tx,scope);err!=nil{return zero,err}
 if err=repository.lock(ctx,tx);err!=nil{return zero,err}
 var registered bool
 if err=tx.QueryRow(ctx,"SELECT photo.register_v29_manual_perspective_replacement_transport($1,$2,$3,$4::jsonb,$5::jsonb,$6)",p.tenant,saved.Identity.Run,string(descriptor),proof,string(transport),string(manifest)).Scan(&registered);err!=nil{return zero,err}
 if !registered{return zero,v2input.ErrExpired}
 if err=ctx.Err();err!=nil{return zero,err}
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 return graph,nil
}
