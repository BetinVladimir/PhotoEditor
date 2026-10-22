package review

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
)
type LocalPortraitDiscovery struct {RunID uuid.UUID `json:"run_id"`;WorkflowID *uuid.UUID `json:"workflow_id"`}
func(s *Service)DiscoverLocalPortraitResult(ctx context.Context,who auth.Identity,tid,run uuid.UUID)(LocalPortraitDiscovery,error){
 var out LocalPortraitDiscovery
 if err:=ctx.Err();err!=nil{return out,err}
 if run==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 if s==nil||s.DB==nil{return out,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  err:=tx.QueryRow(ctx,`SELECT run_id,workflow_id FROM photo.discover_v28_local_portrait_result($1,$2,$3,$4)`,tid,run,who.UserID,who.SessionID).Scan(&out.RunID,&out.WorkflowID)
  if errors.Is(err,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if err!=nil{return err}
  if out.RunID!=run||out.WorkflowID!=nil&&*out.WorkflowID==uuid.Nil{return domain.NewError("INPUT_INVALID",422)};return ctx.Err()
 });if err!=nil{return LocalPortraitDiscovery{},err};if err=ctx.Err();err!=nil{return LocalPortraitDiscovery{},err};return out,nil
}
