package run

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// This private mode is constructed by the authored parent server compiler only.
// Public Apply JSON cannot select it. All ordinary accepted-style/runtime,
// snapshot, project, masks and quota checks in Service.apply remain in force.
// It does not use editor_effect_builds or an EditorExecutedBaseReceipt.
type authoredBaseApplyBinding struct {
 ParentRunID uuid.UUID `json:"parent_run_id"`
 ProgramSHA string `json:"program_sha"`
 PhasePlanSHA string `json:"phase_plan_sha"`
 OriginalSource v2input.SessionVersionBinding `json:"original_source"`
 EffectiveStyle v2input.Ref `json:"effective_style"`
}
func(m *editorEffectApplyMode)authorizeAuthoredBase(ctx context.Context,tx pgx.Tx,who auth.Identity,tid uuid.UUID)error{
 origin:=m.AuthoredBase
 if origin==nil||origin.ParentRunID==uuid.Nil||!isLowerHexSHA(origin.ProgramSHA)||!isLowerHexSHA(origin.PhasePlanSHA)||m.Binding!=origin.OriginalSource||m.ExecutedStyle!=origin.EffectiveStyle||m.Precompose{
  return domain.NewError("VALIDATION_FAILED",422)
 }
 var valid bool
 err:=tx.QueryRow(ctx,`SELECT photo.authored_render_base_origin_scope($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,tid,origin.ParentRunID,m.ProjectID,who.UserID,who.SessionID,origin.ProgramSHA,origin.PhasePlanSHA,m.FrozenRecipe.ArtifactID,m.FrozenRecipe.SHA256,m.ExecutedStyle.ArtifactID,m.ExecutedStyle.SHA256).Scan(&valid)
 if err!=nil{return err};if !valid{return domain.NewError("SOURCE_VERSION_MISMATCH",409)};return nil
}
func(m *editorEffectApplyMode)commitAuthoredBase(ctx context.Context,tx pgx.Tx,who auth.Identity,tid uuid.UUID,accepted ApplyAccepted)error{
 if accepted.RunID==uuid.Nil||accepted.RunID==m.AuthoredBase.ParentRunID{return domain.NewError("VALIDATION_FAILED",422)}
 var valid bool
 err:=tx.QueryRow(ctx,`SELECT photo.bind_authored_render_base_child($1,$2,$3,$4,$5,$6,$7)`,tid,m.AuthoredBase.ParentRunID,accepted.RunID,m.ProjectID,who.UserID,who.SessionID,m.AuthoredBase.ProgramSHA).Scan(&valid)
 if err!=nil{return err};if !valid{return domain.NewError("SOURCE_VERSION_MISMATCH",409)};return nil
}
