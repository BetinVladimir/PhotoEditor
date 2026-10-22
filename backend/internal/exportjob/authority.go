package exportjob

import (
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

// Re-read the tenant role before applying project rules. In particular an old
// admin role must not survive a snapshot/preset/receipt row wait.
func currentExportProject(ctx context.Context,tx pgx.Tx,who auth.Identity,tid,pid uuid.UUID)(project.Project,error){
 role,err:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Viewer)
 if err!=nil{return project.Project{},err}
 p,err:=project.Authorize(ctx,tx,tid,pid,who.UserID,role,domain.Editor,false)
 if err!=nil{return p,err}
 if !p.DownloadAllowed{return p,domain.NewError("DOWNLOAD_FORBIDDEN",403)}
 return p,ctx.Err()
}
