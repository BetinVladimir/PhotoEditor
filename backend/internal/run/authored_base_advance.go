package run

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// AdvanceAuthoredBase mounts the actual guarded Apply producer. The scheduler
// cannot supply a replacement request/style: the closed context comes only from
// the retained parent, and BeginAuthoredBase independently rechecks it in API SQL.
// Repeated calls use one deterministic child key; an accepted retry is not
// reported as new maintenance progress.
func(s EditorEffectsService)AdvanceAuthoredBase(ctx context.Context,scheduler *pgxpool.Pool,tid,parent uuid.UUID)(bool,error){
 if scheduler==nil||tid==uuid.Nil||parent==uuid.Nil{return false,domain.NewError("VALIDATION_FAILED",422)}
 var raw []byte
 if err:=scheduler.QueryRow(ctx,`SELECT photo.authored_render_parent_base_context($1,$2)`,tid,parent).Scan(&raw);err!=nil{return false,err}
 if len(raw)==0||bytes.Equal(raw,[]byte("null")){return false,nil}
 var bound struct {
  Parent uuid.UUID `json:"parent"`
  Tenant uuid.UUID `json:"tenant"`
  Project uuid.UUID `json:"project"`
  Actor uuid.UUID `json:"actor"`
  Session uuid.UUID `json:"session"`
  Program string `json:"program_sha"`
  Plan string `json:"phase_plan_sha"`
  Style v2input.Ref `json:"effective_style"`
  Intent EditorEffectRequest `json:"intent"`
 }
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&bound)!=nil{return false,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 var tail any;if decoder.Decode(&tail)!=io.EOF{return false,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 if bound.Parent!=parent||bound.Tenant!=tid||bound.Actor==uuid.Nil||bound.Session==uuid.Nil||bound.Project==uuid.Nil||!isLowerHexSHA(bound.Program)||!isLowerHexSHA(bound.Plan){return false,domain.NewError("SOURCE_VERSION_MISMATCH",409)}
 response,err:=s.BeginAuthoredBase(ctx,auth.Identity{UserID:bound.Actor,SessionID:bound.Session},tid,bound.Project,"authored-base/"+parent.String(),AuthoredBaseRequest{Intent:bound.Intent,ParentRunID:parent,ProgramSHA:bound.Program,PhasePlanSHA:bound.Plan,EffectiveStyle:bound.Style})
 if err!=nil{return false,err}
 if response.ResourceID==nil||!validEditorEffectReceipt(response.Status,response.Body,*response.ResourceID){return false,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 return !response.Replayed,nil
}
