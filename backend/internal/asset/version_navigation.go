package asset

import (
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

// VersionNavigation resolves an exact historical version, never the latest
// version of an asset. It grants no mutation or object-download authority.
type VersionNavigation struct {
 TenantID uuid.UUID `json:"tenant_id"`
 ProjectID uuid.UUID `json:"project_id"`
 AssetID uuid.UUID `json:"asset_id"`
 AssetVersionID uuid.UUID `json:"asset_version_id"`
}
func (s Service) GetVersionNavigation(ctx context.Context,who auth.Identity,tid,pid,version uuid.UUID)(VersionNavigation,error){
 var out VersionNavigation
 if err:=ctx.Err();err!=nil{return out,err}
 if tid==uuid.Nil||pid==uuid.Nil||version==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 bounded,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 err:=tenant.WithTenant(bounded,s.DB,who,tid,domain.Viewer,func(tx pgx.Tx,role domain.Role)error{
  if _,err:=project.Authorize(bounded,tx,tid,pid,who.UserID,role,domain.Viewer,false);err!=nil{return err}
  err:=tx.QueryRow(bounded,`SELECT a.tenant_id,a.project_id,a.id,v.id
   FROM photo.asset_versions v
   JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   JOIN photo.artifacts master ON master.tenant_id=v.tenant_id AND master.id=v.master_artifact_id
   WHERE v.tenant_id=$1 AND a.project_id=$2 AND v.id=$3
    AND a.deleted_at IS NULL AND a.upload_state='verified' AND v.review_state='accepted'
    AND master.state='ready' AND master.verified_at IS NOT NULL`,tid,pid,version).Scan(&out.TenantID,&out.ProjectID,&out.AssetID,&out.AssetVersionID)
  if errors.Is(err,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)}
  if err!=nil{return err}
  role,err=tenant.AuthorizeInTransaction(bounded,tx,who,tid,domain.Viewer);if err!=nil{return err}
  if _,err=project.Authorize(bounded,tx,tid,pid,who.UserID,role,domain.Viewer,false);err!=nil{return err}
  return bounded.Err()
 })
 if err!=nil{return VersionNavigation{},err};if err=bounded.Err();err!=nil{return VersionNavigation{},err};return out,nil
}
