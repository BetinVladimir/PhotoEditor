package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// ReadPublishedManualPerspectiveReplacementTransport reads only existing blocked-task
// transport and a live output reservation. No object IO, staging, quota repair
// or registration is initiated by Go. SQL refuses missing task/reservation
// before invoking its exact replay validators.
func(p PreparedNegativeReplacementInputs)ReadPublishedManualPerspectiveReplacementTransport(ctx context.Context,db NegativeTransactions,bucket string,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,draft PerspectiveReplacementDraft,snapshot uuid.UUID,located LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity PerspectiveResourceEstimate,published v2input.Ref)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||bucket!=p.bucket||len(bucket)<3||len(bucket)>63{return zero,v2input.ErrInvalid}
 r:=manualPerspectiveReplacementParamsRepository{db:db,bucket:bucket,prepared:p,draft:draft,snapshot:snapshot,located:located,locks:locks,maxPixels:maxPixels,capacity:capacity,rebuilt:append([]VerifiedAdoptedParentArrays(nil),rebuilt...),retained:append([]VerifiedAdoptedParentArrays(nil),retained...),mappings:append([]VerifiedNegativeAffectedSources(nil),mappings...),unchanged:append([]VerifiedNegativeUnchangedArrays(nil),unchanged...)}
 descriptor,err:=r.descriptor(ctx,published);if err!=nil{return zero,err}
 saved,plan,err:=r.document(ctx);if err!=nil{return zero,err}
 scope:=artifactjson.Scope{Tenant:p.tenant,Run:saved.Identity.Run,Owner:saved.Identity.Task,Purpose:"task_params"}
 proof,err:=r.proof(ctx,scope,published.SHA256,int64(len(plan.Params)));if err!=nil{return zero,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err=r.authorize(ctx,tx,scope);err!=nil{return zero,err}
 if err=r.lock(ctx,tx);err!=nil{return zero,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_manual_perspective_replacement_published_transport($1,$2,$3,$4,$5::jsonb)",p.tenant,saved.Identity.Run,saved.Identity.Task,string(descriptor),proof).Scan(&raw);err!=nil{return zero,err}
 // The shared hash verifier expects four roots; manual perspective has Params,
 // WorkingImage, Calibration and Report. Auto mode needs its own lines gate.
 graph,err:=restoreNegativePublishedTransport(ctx,raw,func(ctx context.Context)([]byte,error){return r.descriptor(ctx,published)});if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 return graph,nil
}
