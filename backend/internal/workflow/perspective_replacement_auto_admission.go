package workflow

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

// AdmitAutomaticPerspectiveGraph registers exact inputs and repeats complete
// current source/segment/qualification authority through atomic graph seal and
// durable queued receipt. A commit error is ambiguous; callers recover receipt.
func(r ReplacementDetectionDocuments) AdmitAutomaticPerspectiveGraph(ctx context.Context,draft v2optical.PerspectiveReplacementDraft,segments ReadyReplacementSegments,located v2optical.LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity v2optical.PerspectiveResourceEstimate)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 if err:=ctx.Err();err!=nil{return zero,err}
 graph,err:=r.RegisterAutomaticPerspectiveInputs(ctx,draft,segments,located,locks,maxPixels,capacity);if err!=nil{return zero,err}
 repo:=automaticPerspectiveParamsRepository{source:r,segments:segments,located:located,locks:locks,maxPixels:maxPixels,capacity:capacity,draft:draft}
 scope,_,_,_,err:=repo.document();if err!=nil{return zero,err}
 descriptor,err:=draft.AutomaticStageDescriptor(ctx,located);if err!=nil{return zero,err}
 if graph.ID!=scope.Owner{return zero,v2input.ErrInvalid}
 err=repo.transaction(ctx,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var admitted bool
  err:=tx.QueryRow(ctx,"SELECT photo.seal_v29_auto_perspective_replacement_graph($1,$2,$3,$4,$5::jsonb)",scope.Tenant,scope.Run,scope.Owner,string(descriptor),string(proof)).Scan(&admitted)
  if err!=nil{return err};if !admitted{return v2input.ErrExpired};return nil
 })
 if err!=nil{return zero,err};return graph,nil
}
