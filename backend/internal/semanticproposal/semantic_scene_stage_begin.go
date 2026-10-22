package semanticproposal

import (
 "context"
 "encoding/json"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Caller owns the original parent advisory lock and rollback. All parent,
// source, published Params and independent producer qualification barriers are
// retained until commit. This only creates a blocked draft, never queues it.
func(s SQLTransferSourceReader) beginSemanticSceneStage(ctx context.Context,tx pgx.Tx,who auth.Identity,parent detectorDraftPlan,plan semanticSceneStagePlan)(DraftAdmission,error){
 var zero DraftAdmission
 if tx==nil{return zero,v2input.ErrInvalid}
 if err:=verifyDetectorPlanSeal(ctx,parent);err!=nil{return zero,err}
 rebuilt,err:=s.compileSemanticSceneStagePlan(ctx,who,parent.request,plan.prepared,plan.shell.Bytes)
 if err!=nil{return zero,err}
 if rebuilt.seal!=plan.seal{return zero,v2input.ErrConflict}
 p:=rebuilt.prepared.published
 // Full binding membership is proved from the parent's immutable snapshot,
 // never merely by sharing a project or accepted asset version.
 targets,err:=BindTargets(parent.request,parent.anchor.snapshotRef,parent.anchor.snapshotBody)
 if err!=nil{return zero,err}
 found:=p.anchor.source==parent.request.Intent.Reference
 for _,binding:=range targets{if binding==p.anchor.source{found=true}}
 if !found{return zero,v2input.ErrConflict}
 if err:=LockInventoryInTransaction(ctx,tx,who,parent.request,parent.published.Inventory);err!=nil{return zero,err}
 if err:=LockPublishedIntentInTransaction(ctx,tx,who,parent.request,parent.published);err!=nil{return zero,err}
 if err:=LockSnapshotInTransaction(ctx,tx,who,parent.request,parent.anchor);err!=nil{return zero,err}
 if err:=s.lockSourcePreparationProof(ctx,tx,who,parent.request.TenantID,p.anchor.source,p.source);err!=nil{return zero,err}
 if err:=s.lockSceneProducerQualification(ctx,tx,who,parent.request.TenantID,p.qualification);err!=nil{return zero,err}
 var evidence []v2draft.InputEvidence
 if json.Unmarshal([]byte(rebuilt.prepared.inputs),&evidence)!=nil{return zero,v2input.ErrInvalid}
 var pin []byte
 for _,entry:=range evidence {
  if entry.Contract!="Params"{continue}
  object:=entry.Object
  if pin!=nil || entry.Ref!=p.ref || object.Bytes!=int64(len(p.body)) || object.SHA256!=p.ref.SHA256 || object.ContentType!="application/json"{return zero,v2input.ErrInvalid}
  pin,err=canonical.Marshal(map[string]any{"bucket":s.Bucket,"key":object.Key,"version":object.VersionID,"sha256":object.SHA256,"bytes":object.Bytes,"media_type":object.ContentType})
  if err!=nil{return zero,err}
 }
 if pin==nil{return zero,v2input.ErrInvalid}
 var ready bool
 b:=p.anchor.source
 if err:=tx.QueryRow(ctx,"SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)",parent.request.TenantID,s.ProjectID,who.UserID,who.SessionID,b.AssetVersionID,b.Source.ArtifactID,b.Source.SHA256,p.anchor.document,p.ref.ArtifactID,p.ref.SHA256,string(pin)).Scan(&ready);err!=nil{return zero,err}
 if !ready{return zero,v2input.ErrConflict}
 if err:=ctx.Err();err!=nil{return zero,err}
 outcome,run,err:=v2input.BeginAdmissionInTransaction(ctx,tx,rebuilt.shell,rebuilt.key,parent.request.RequestSHA)
 if err!=nil{return zero,err}
 if run!=rebuilt.shell.Run || (outcome!="created" && outcome!="pending" && outcome!="replayed"){return zero,v2input.ErrConflict}
 // Admission may wait for the receipt, usage or existing shell. Do not commit
 // a new reservation after evidence expired during one of those waits. All
 // previously acquired proof locks stay held until the caller commits.
 if err:=LockInventoryInTransaction(ctx,tx,who,parent.request,parent.published.Inventory);err!=nil{return zero,err}
 if err:=LockPublishedIntentInTransaction(ctx,tx,who,parent.request,parent.published);err!=nil{return zero,err}
 if err:=LockSnapshotInTransaction(ctx,tx,who,parent.request,parent.anchor);err!=nil{return zero,err}
 if err:=s.lockSourcePreparationProof(ctx,tx,who,parent.request.TenantID,p.anchor.source,p.source);err!=nil{return zero,err}
 if err:=s.lockSceneProducerQualification(ctx,tx,who,parent.request.TenantID,p.qualification);err!=nil{return zero,err}
 ready=false
 if err:=tx.QueryRow(ctx,"SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)",parent.request.TenantID,s.ProjectID,who.UserID,who.SessionID,b.AssetVersionID,b.Source.ArtifactID,b.Source.SHA256,p.anchor.document,p.ref.ArtifactID,p.ref.SHA256,string(pin)).Scan(&ready);err!=nil{return zero,err}
 if !ready{return zero,v2input.ErrConflict}
 return DraftAdmission{RunID:run,Outcome:outcome},ctx.Err()
}
