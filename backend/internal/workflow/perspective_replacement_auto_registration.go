package workflow

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

// RegisterAutomaticPerspectiveInputs recovers only existing blocked transport,
// validates its hashes against the qualified descriptor and registers exact
// inputs. Missing tasks/reservations/documents are never created by this method.
func(r ReplacementDetectionDocuments) RegisterAutomaticPerspectiveInputs(ctx context.Context,draft v2optical.PerspectiveReplacementDraft,segments ReadyReplacementSegments,located v2optical.LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity v2optical.PerspectiveResourceEstimate)(v2input.GraphTask,error){
 var out v2input.GraphTask
 if err:=ctx.Err();err!=nil{return out,err}
 repo:=automaticPerspectiveParamsRepository{source:r,segments:segments,located:located,locks:locks,maxPixels:maxPixels,capacity:capacity,draft:draft}
 scope,_,_,_,err:=repo.document();if err!=nil{return out,err}
 descriptor,err:=draft.AutomaticStageDescriptor(ctx,located);if err!=nil{return out,err}
 err=repo.transaction(ctx,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var raw []byte
  if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_auto_perspective_replacement_published_transport($1,$2,$3,$4,$5::jsonb)",scope.Tenant,scope.Run,scope.Owner,string(descriptor),string(proof)).Scan(&raw);err!=nil{return err}
  graph,err:=draft.RestoreAutomaticTransport(ctx,located,raw);if err!=nil{return err}
  transport,err:=canonical.Marshal(struct{Manifest v2input.Ref `json:"manifest"`;Chunks []v2input.Ref `json:"chunks"`;Count int `json:"count"`;SHA string `json:"sha256"`}{graph.Built.Manifest,graph.Built.Chunks,graph.Built.Count,graph.Built.SHA256});if err!=nil{return err}
  manifest,err:=canonical.Marshal(v2input.Manifest{SchemaVersion:1,TaskID:graph.ID,EntryCount:graph.Built.Count,EntriesSHA:graph.Built.SHA256,Chunks:graph.Built.Chunks});if err!=nil{return err}
  var registered bool
  err=tx.QueryRow(ctx,"SELECT photo.register_v29_auto_perspective_replacement_transport($1,$2,$3,$4::jsonb,$5::jsonb,$6)",scope.Tenant,scope.Run,string(descriptor),string(proof),string(transport),string(manifest)).Scan(&registered)
  if err!=nil{return err};if !registered{return v2input.ErrExpired}
  out=graph;return nil
 });if err!=nil{return v2input.GraphTask{},err};return out,nil
}
