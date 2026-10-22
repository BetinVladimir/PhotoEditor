package run

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

type EditorEffectStatus struct {
 BuildRunID uuid.UUID `json:"build_run_id"`
 State string `json:"state"`
 BuildStatus string `json:"build_status"`
 SealRunID *uuid.UUID `json:"seal_run_id"`
 FinalRunID *uuid.UUID `json:"final_run_id"`
 ReviewRequired bool `json:"review_required"`
}
// Status never equates the intermediate Apply completion with client render
// completion. Recovery remains bound to the original actor and frozen source.
func(s EditorEffectsService)Status(ctx context.Context,who auth.Identity,tid,pid,runID uuid.UUID)(EditorEffectStatus,error){
 var out EditorEffectStatus
 err:=tenant.WithTenant(ctx,s.Runs.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,err:=project.Authorize(ctx,tx,tid,pid,who.UserID,role,domain.Editor,false);err!=nil{return err}
  var mode editorEffectApplyMode;mode.ProjectID=pid;mode.ItemID=uuid.NewSHA1(runID,[]byte("scope-validation"))
  var actor uuid.UUID
  err:=tx.QueryRow(ctx,`SELECT o.run_id,o.state,r.status,o.seal_run_id,o.final_run_id,
   EXISTS(SELECT FROM photo.reviews review WHERE review.tenant_id=o.tenant_id AND review.state='pending' AND review.run_id IN(o.run_id,o.seal_run_id,o.final_run_id)),
   o.actor_id,o.asset_version_id,o.source_artifact_id,o.source_sha,o.source_canvas_hash,o.frozen_recipe_artifact_id,o.frozen_recipe_sha,o.frozen_document_id,o.draft_document_id,o.draft_revision
   FROM photo.editor_effect_builds o JOIN photo.runs r ON r.tenant_id=o.tenant_id AND r.id=o.run_id
   WHERE o.tenant_id=$1 AND o.project_id=$2 AND o.run_id=$3`,tid,pid,runID).Scan(&out.BuildRunID,&out.State,&out.BuildStatus,&out.SealRunID,&out.FinalRunID,&out.ReviewRequired,&actor,&mode.Binding.AssetVersionID,&mode.Binding.Source.ArtifactID,&mode.Binding.Source.SHA256,&mode.Binding.CanvasHash,&mode.FrozenRecipe.ArtifactID,&mode.FrozenRecipe.SHA256,&mode.FrozenDocumentID,&mode.DraftDocumentID,&mode.DraftRevision)
  if errors.Is(err,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if err!=nil{return err};if actor!=who.UserID{return domain.NewError("FORBIDDEN",403)}
  // Withdrawal is triggered by a stale source/revision. Its terminal receipt
  // must remain readable by the same actor with current project access, or an
  // accepted journal could never be acknowledged. No artifact is returned and
  // every live/complete origin still requires the full source fence below.
  // Re-read current role/project after the awaited build query, including the
  // terminal path which intentionally does not require a still-current draft.
  currentRole,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e}
  if _,e=project.Authorize(ctx,tx,tid,pid,who.UserID,currentRole,domain.Editor,false);e!=nil{return e}
  if out.State=="failed"{return nil}
  if e=mode.authorize(ctx,tx,who,tid,false);e!=nil{return e}
  currentRole,e=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e}
  _,e=project.Authorize(ctx,tx,tid,pid,who.UserID,currentRole,domain.Editor,false);return e
 });return out,err
}
