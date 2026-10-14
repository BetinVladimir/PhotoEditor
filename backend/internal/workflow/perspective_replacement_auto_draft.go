package workflow

import (
 "context"
 "encoding/json"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

// LockAutomaticPerspectiveDraft composes current detector/result/ready segment,
// mapped physical-source and live qualification authority in caller transaction.
// No draft is saved or admitted here; the auto-specific SQL path is separate.
func(r ReplacementDetectionDocuments) LockAutomaticPerspectiveDraft(ctx context.Context,tx pgx.Tx,segments ReadyReplacementSegments,located v2optical.LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity v2optical.PerspectiveResourceEstimate)(v2optical.PerspectiveReplacementDraft,error){
 var zero v2optical.PerspectiveReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if tx==nil||segments.bucket!=r.Bucket{return zero,v2input.ErrInvalid}
 fresh,err:=r.LockReadySegments(ctx,tx,segments.plan);if err!=nil{return zero,err}
 if fresh!=segments{return zero,v2input.ErrExpired}
 step,_,err:=r.Prepared.PerspectiveDetectionCurrentProof(ctx,tx,r.Rebuilt,r.Retained,r.Mappings,r.Unchanged,r.Plan.Descriptor(),r.Snapshot);if err!=nil{return zero,err}
 if _,err=RestorePerspectiveReplacementDetectionPlan(ctx,step,r.Plan.Descriptor());err!=nil{return zero,err}
 draft,err:=step.CompileLockedAutomaticPerspectiveReplacement(ctx,tx,r.Snapshot,located,locks,maxPixels,capacity,segments.Ref(),segments.Bytes());if err!=nil{return zero,err}
 proof,err:=(replacementSegmentsRepository{source:r,plan:segments.plan}).lockedProof(ctx,tx);if err!=nil{return zero,err}
 var valid bool
 err=tx.QueryRow(ctx,"SELECT photo.lock_v29_auto_perspective_replacement_identity($1,$2,$3::jsonb,$4)",segments.plan.Scope().Tenant,string(draft.Bytes()),string(proof),r.Bucket).Scan(&valid)
 if err!=nil{return zero,err};if !valid{return zero,v2input.ErrExpired}
 return draft,ctx.Err()
}

// SaveAutomaticPerspectiveDraft recompiles current qualification and physical
// authority, then saves exact immutable bytes in the same bounded transaction.
// An existing conflicting run is refused; no task or queue is created here.
func(r ReplacementDetectionDocuments) SaveAutomaticPerspectiveDraft(ctx context.Context,segments ReadyReplacementSegments,located v2optical.LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity v2optical.PerspectiveResourceEstimate)(v2optical.PerspectiveReplacementDraft,error){
 var out v2optical.PerspectiveReplacementDraft
 err:=r.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  draft,err:=r.LockAutomaticPerspectiveDraft(ctx,tx,segments,located,locks,maxPixels,capacity);if err!=nil{return err}
  proof,err:=(replacementSegmentsRepository{source:r,plan:segments.plan}).lockedProof(ctx,tx);if err!=nil{return err}
  descriptor,err:=decodeReplacementDetectionDescriptor(r.Plan.Descriptor());if err!=nil{return err}
  var packet struct{Detection struct{Plan json.RawMessage `json:"plan"`} `json:"detection"`}
  if json.Unmarshal(proof,&packet)!=nil||len(packet.Detection.Plan)<2{return v2input.ErrInvalid}
  var saved bool
  err=tx.QueryRow(ctx,"SELECT photo.save_v29_auto_perspective_replacement_draft($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9::jsonb,$10)",descriptor.Tenant,descriptor.Project,descriptor.Actor,descriptor.Session,descriptor.Review,descriptor.DecisionHash,string(packet.Detection.Plan),string(draft.Bytes()),string(proof),r.Bucket).Scan(&saved)
  if err!=nil{return err};if !saved{return v2input.ErrExpired}
  out=draft;return nil
 })
 if err!=nil{return v2optical.PerspectiveReplacementDraft{},err};return out,nil
}

// LoadAutomaticPerspectiveDraft recompiles the expected draft under current
// physical/qualification locks and reads exact saved bytes. It never saves.
func(r ReplacementDetectionDocuments) LoadAutomaticPerspectiveDraft(ctx context.Context,segments ReadyReplacementSegments,located v2optical.LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity v2optical.PerspectiveResourceEstimate)(v2optical.PerspectiveReplacementDraft,error){
 var out v2optical.PerspectiveReplacementDraft
 err:=r.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  draft,err:=r.LockAutomaticPerspectiveDraft(ctx,tx,segments,located,locks,maxPixels,capacity);if err!=nil{return err}
  proof,err:=(replacementSegmentsRepository{source:r,plan:segments.plan}).lockedProof(ctx,tx);if err!=nil{return err}
  var saved *string
  err=tx.QueryRow(ctx,"SELECT photo.read_v29_auto_perspective_replacement_draft($1,$2,$3::jsonb,$4)",segments.plan.Scope().Tenant,string(draft.Bytes()),string(proof),r.Bucket).Scan(&saved)
  if err!=nil{return err}
  if saved==nil||*saved!=string(draft.Bytes()){return v2input.ErrExpired}
  out=draft;return nil
 })
 if err!=nil{return v2optical.PerspectiveReplacementDraft{},err};return out,nil
}
