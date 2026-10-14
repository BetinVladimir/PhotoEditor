package workflow

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

// ReadAutomaticPerspectiveReceipt is read-only recovery of admission under
// current physical/segment/qualification authority; it never stages or seals.
func(r ReplacementDetectionDocuments) ReadAutomaticPerspectiveReceipt(ctx context.Context,segments ReadyReplacementSegments,located v2optical.LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity v2optical.PerspectiveResourceEstimate)(v2optical.AutomaticPerspectiveReplacementReceipt,error){
 var out v2optical.AutomaticPerspectiveReplacementReceipt
 err:=r.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  draft,err:=r.LockAutomaticPerspectiveDraft(ctx,tx,segments,located,locks,maxPixels,capacity);if err!=nil{return err}
  descriptor,err:=decodeReplacementDetectionDescriptor(r.Plan.Descriptor());if err!=nil{return err}
  repo:=automaticPerspectiveParamsRepository{source:r,draft:draft}
  scope,_,_,_,err:=repo.document();if err!=nil{return err}
  proof,err:=(replacementSegmentsRepository{source:r,plan:segments.plan}).lockedProof(ctx,tx);if err!=nil{return err}
  var raw []byte
  err=tx.QueryRow(ctx,"SELECT photo.read_v29_auto_perspective_replacement_queued_receipt($1,$2,$3,$4,$5,$6::jsonb,$7)",scope.Tenant,scope.Run,descriptor.Actor,descriptor.Session,string(draft.Bytes()),string(proof),r.Bucket).Scan(&raw)
  if err!=nil{return err};if len(raw)<2{return v2input.ErrExpired}
  out,err=draft.ParseAutomaticQueuedReceipt(ctx,located,raw);return err
 })
 if err!=nil{return v2optical.AutomaticPerspectiveReplacementReceipt{},err};return out,nil
}
