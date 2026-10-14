package interchange

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type SQLExportSnapshotAuthority struct {
 DB *pgxpool.Pool
 Bucket string
 Objects v2input.SessionAnalysisObjectReader
}
// CheckExportSnapshot verifies exact versions, never asset heads. Object IO is
// outside tenant transactions; a second lookup checks the versioned object pins.
func(s SQLExportSnapshotAuthority)CheckExportSnapshot(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,compiled CompiledExportSnapshot)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||s.Objects==nil||len(s.Bucket)<3||len(s.Bucket)>63||tid==uuid.Nil||pid==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return v2input.ErrInvalid}
 raw:=compiled.Body()
 snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(v2input.Ref{ArtifactID:compiled.ID(),SHA256:rawSHA(raw)},raw);if err!=nil{return err}
 for _,item:=range snapshot.Items{
  if err:=ctx.Err();err!=nil{return err}
  object,err:=s.source(ctx,who,tid,pid,item);if err!=nil{return err}
  image,err:=v2input.ReadVerifiedWorkingImage(ctx,s.Objects,object,item.Binding.Source);if err!=nil{return err}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return err};if canvas!=item.Binding.CanvasHash{return v2input.ErrInvalid}
  resolver:=v2draft.ClosureResolver{Catalog:v2draft.SQLArtifactCatalog{DB:s.DB,Bucket:s.Bucket,ProjectID:pid,AssetVersionID:item.Binding.AssetVersionID},Objects:s.Objects}
  if _,err:=resolver.ResolveTaskInputs(ctx,who,tid,[]v2input.Entry{{Ref:item.Recipe,Contract:"FrozenRenderRecipe"},{Ref:item.WorkingImage,Contract:"WorkingImage"}},item.Binding);err!=nil{return err}
  current,err:=s.source(ctx,who,tid,pid,item);if err!=nil{return err};if current!=object{return objectstore.ErrIntegrity}
 }
 return ctx.Err()
}
func(s SQLExportSnapshotAuthority)source(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,item v2input.SessionSnapshotItem)(objectstore.Object,error){
 var object objectstore.Object
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,err:=project.Authorize(ctx,tx,tid,pid,who.UserID,role,domain.Editor,false);err!=nil{return err}
  var bucket string
  err:=tx.QueryRow(ctx,`SELECT m.bucket,m.object_key,m.object_version,m.bytes,m.media_type,m.sha256
   FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   JOIN photo.artifacts m ON m.tenant_id=v.tenant_id AND m.id=v.master_artifact_id AND m.sha256=v.content_hash
   JOIN photo.projects p ON p.tenant_id=a.tenant_id AND p.id=a.project_id JOIN photo.tenants t ON t.id=p.tenant_id
   WHERE v.tenant_id=$1 AND v.id=$3 AND a.project_id=$2 AND a.deleted_at IS NULL AND a.upload_state='verified'
    AND v.review_state='accepted' AND m.id=$4 AND m.sha256=$5 AND m.output_role='working_image'
    AND m.state='ready' AND m.verified_at IS NOT NULL AND m.media_type='application/json' AND m.bytes BETWEEN 2 AND 1048576
    AND m.object_version IS NOT NULL AND m.object_version NOT IN('','null') AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp())
    AND p.archived_at IS NULL AND p.download_allowed AND t.status='active' FOR SHARE OF v,a,m,p,t`,tid,pid,item.Binding.AssetVersionID,item.Binding.Source.ArtifactID,item.Binding.Source.SHA256).Scan(&bucket,&object.Key,&object.VersionID,&object.Bytes,&object.ContentType,&object.SHA256)
  if err!=nil{return err};if bucket!=s.Bucket||object.Key==""{return objectstore.ErrIntegrity};return nil
 })
 if err!=nil{return objectstore.Object{},err};return object,nil
}
var _ ExportSnapshotAuthority=SQLExportSnapshotAuthority{}
