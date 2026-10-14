package upload

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/rawdevelopment"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2qualification"
)
type RAWCapabilities struct {
 ProjectID uuid.UUID `json:"project_id"`
 Available bool `json:"available"`
 Development *rawdevelopment.CapabilityView `json:"development,omitempty"`
}
type rawCapabilityLoader interface {
 Load(context.Context,rawdevelopment.ReportRows,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID)(*rawdevelopment.Capability,error)
}
// RAWCapabilities returns only currently qualified settings. It is not an
// admission ticket: Begin independently revalidates the physical report.
func (s Service) RAWCapabilities(ctx context.Context,who auth.Identity,tenantID,projectID uuid.UUID)(RAWCapabilities,error){
 out:=RAWCapabilities{ProjectID:projectID}
 if projectID==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 if s.DB==nil{return out,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 err:=tenant.WithTenant(ctx,s.DB,who,tenantID,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,err:=project.Authorize(ctx,tx,tenantID,projectID,who.UserID,role,domain.Editor,true);err!=nil{return err}
  loader,ok:=s.RAWAdmission.(rawCapabilityLoader)
  if !ok{return nil}
  capability,err:=loader.Load(ctx,tx,tenantID,projectID,who.UserID,who.SessionID)
  if errors.Is(err,v2qualification.ErrUnqualified){return nil}
  if err!=nil{return err}
  view,available:=capability.View()
  if available{out.Available=true;out.Development=&view}
  return ctx.Err()
 })
 if err!=nil{return RAWCapabilities{},err}
 return out,nil
}
