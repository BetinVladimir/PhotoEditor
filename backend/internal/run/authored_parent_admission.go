package run

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

// AuthoredParentAccepted belongs to the explicit append-only phase orchestrator.
// It is not an ordinary compose/1 task or an Apply output collection receipt.
type AuthoredParentAccepted struct {RunID uuid.UUID `json:"run_id"`;Status string `json:"status"`;Replayed bool `json:"-"`}

// admitAuthoredParent persists preparation under current locked Editor origin.
// It reserves no fictional future output bytes: each real producer child must
// independently reserve its calibrated outputs before materialization/dispatch.
func(s EditorEffectsService)admitAuthoredParent(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,p PreparedAuthoredParent)(AuthoredParentAccepted,error){
 if p.mode==nil||p.Parent==uuid.Nil||p.Workflow==uuid.Nil||p.Program.Workflow!=p.Workflow||p.Intent!=p.mode.Intent||p.mode.AuthoredBase!=nil||p.mode.Precompose{return AuthoredParentAccepted{},domain.NewError("VALIDATION_FAILED",422)}
 var accepted AuthoredParentAccepted
 err:=tenant.WithTenant(ctx,s.Runs.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  // Match production admission's tenant -> usage -> domain locks order. Taking
  // source/actor locks before usage would invert concurrent child settlement.
  if _,e:=tx.Exec(ctx,`SELECT id FROM photo.tenants WHERE id=$1 AND status='active' FOR UPDATE`,tid);e!=nil{return e}
  var usageTenant uuid.UUID
  if e:=tx.QueryRow(ctx,`SELECT tenant_id FROM photo.tenant_usage WHERE tenant_id=$1 FOR UPDATE`,tid).Scan(&usageTenant);e!=nil{return e}
  if _,e:=project.Authorize(ctx,tx,tid,pid,who.UserID,role,domain.Editor,false);e!=nil{return e}
  // Accepted retries use their original immutable snapshot; a fresh random
  // snapshot would turn a lawful replay into a config collision.
  var existingSnapshot uuid.UUID
  err:=tx.QueryRow(ctx,`SELECT input_snapshot_id FROM photo.runs WHERE tenant_id=$1 AND id=$2`,tid,p.Parent).Scan(&existingSnapshot)
  if err!=nil&&err!=pgx.ErrNoRows{return err}
  if err==nil{
   var raw []byte
   if e:=tx.QueryRow(ctx,`SELECT photo.authored_render_parent_accepted($1,$2,$3,$4,$5)`,tid,p.Parent,pid,who.UserID,who.SessionID).Scan(&raw);e!=nil{return e}
   var stored struct{Parent uuid.UUID `json:"parent_run_id"`;RequestSHA string `json:"request_sha"`}
   if decodeAuthoredJSON(raw,&stored)!=nil||stored.Parent!=p.Parent{return domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
   if stored.RequestSHA!=p.RequestSHA{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   accepted=AuthoredParentAccepted{RunID:p.Parent,Status:"running",Replayed:true};return nil
  }
  if e:=p.mode.authorize(ctx,tx,who,tid,true);e!=nil{return e}
  snapshot:=existingSnapshot
  if err==pgx.ErrNoRows{var e error;snapshot,_,_,e=p.mode.snapshot(ctx,tx,who,tid);if e!=nil{return e}}
  policy:=map[string]any{"schema_version":1,"orchestrator":"authored_render_phases/1","failure_policy":"fail_fast","max_phases":5000}
  policySHA,e:=canonical.Hash(policy);if e!=nil{return e}
  config:=map[string]any{"project_id":pid,"input_snapshot_id":snapshot,"v26_authored":map[string]any{
   "schema_version":1,"auth_session_id":who.SessionID,"request_sha":p.RequestSHA,"submission_key":p.Key,
   "original_recipe":p.Intent.Recipe,"original_source":p.Intent.Source,"recipe_jcs":string(p.Render.Request.RecipeRaw),
   "frozen_document_id":p.Intent.FrozenDocumentID,"draft_document_id":p.Intent.DraftDocumentID,"draft_revision":p.Intent.DraftRevision,
   "layer_stack_jcs":string(p.Render.Request.LayerRaw),"program_sha":p.ProgramSHA,"program_jcs":string(p.ProgramJCS),"base_phase_plan":p.BasePlan,
   "effective_style":p.mode.ExecutedStyle,"qualification":p.Render.Qualification,"final_operation_version":p.Render.OperationVersion,"calibration_sha":p.Render.CalibrationSHA,
   "closure":p.mode.Closure,"workflow_policy":policy}}
  raw,e:=canonical.Marshal(config);if e!=nil{return e}
  var admitted bool
  e=tx.QueryRow(ctx,`SELECT photo.begin_authored_render_parent($1,$2,$3,$4,$5,$6,$7,$8,$9)`,tid,pid,snapshot,who.UserID,who.SessionID,p.Parent,p.Workflow,string(raw),policySHA).Scan(&admitted)
  if e!=nil{return e};if !admitted{return domain.NewError("SOURCE_VERSION_MISMATCH",409)}
  accepted=AuthoredParentAccepted{RunID:p.Parent,Status:"running"};return nil
 })
 return accepted,err
}

// Keep the public result closed when used by the eventual mounted continuation
// service; no map-presence or arbitrary JSON qualifies an accepted parent.
func encodeAuthoredParentAccepted(v AuthoredParentAccepted)(json.RawMessage,error){
 if v.RunID==uuid.Nil||v.Status!="running"{return nil,domain.NewError("VALIDATION_FAILED",422)}
 raw,err:=canonical.Marshal(v);return json.RawMessage(raw),err
}
