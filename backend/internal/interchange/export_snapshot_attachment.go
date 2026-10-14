package interchange
import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
// Uses the same installation and canonical identity checks as publication.
func(s SQLExportSnapshotObjectRepository)AttachExportSnapshot(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key string,p CompiledExportSnapshot,root v2input.Ref)error{
 id,err:=s.validate(ctx,who,tid,pid,key,p);if err!=nil{return err}
 if root.ArtifactID!=id||root.SHA256!=rawSHA(p.Body()){return v2input.ErrInvalid}
 return tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var accepted bool
  if err:=tx.QueryRow(ctx,`SELECT photo.attach_v30_export_snapshot($1,$2,$3,$4,$5,$6,$7,$8,$9)`,tid,who.UserID,who.SessionID,pid,key,p.ID(),id,rawSHA(p.Request()),root.SHA256).Scan(&accepted);err!=nil{return err}
  if !accepted{return artifactjson.ErrStale};return ctx.Err()
 })
}
var _ ExportSnapshotAttachment=SQLExportSnapshotObjectRepository{}
