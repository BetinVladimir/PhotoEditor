package provenance

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type ReadySigningExport struct {
 Tenant,Project,ExportID,Version,Task,Run uuid.UUID
 Ref v2input.Ref
 Object objectstore.Object
}
type SigningExportCatalog interface {
 LookupSigningExport(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(ReadySigningExport,error)
}
// Lineage must resolve only canonical execution evidence for this exact ready
// encode output, including watermark/metadata and generated/review claims.
type FrozenExportLineageSource interface {
 ResolveFrozenExportLineage(context.Context,auth.Identity,ReadySigningExport)(v2input.Ref,[]byte,error)
}
type BoundSigningExportSource struct {Catalog SigningExportCatalog;Lineages FrozenExportLineageSource}
func(s BoundSigningExportSource)ResolveSigningExport(ctx context.Context,who auth.Identity,tid,id uuid.UUID)(SigningExportEvidence,error){
 var zero SigningExportEvidence
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Catalog==nil||s.Lineages==nil||tid==uuid.Nil||id==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return zero,ErrInvalid}
 ready,err:=s.Catalog.LookupSigningExport(ctx,who,tid,id);if err!=nil{return zero,err}
 if ready.Tenant!=tid||ready.ExportID!=id||ready.Project==uuid.Nil||ready.Version==uuid.Nil||ready.Task==uuid.Nil||ready.Run==uuid.Nil||!ref(ready.Ref)||ready.Object.SHA256!=ready.Ref.SHA256||ready.Object.Key==""||ready.Object.VersionID==""||ready.Object.VersionID=="null"||ready.Object.Bytes<1||ready.Object.Bytes>512<<20||ready.Object.ContentType!="image/jpeg"&&ready.Object.ContentType!="image/png"{return zero,ErrInvalid}
 lineage,body,err:=s.Lineages.ResolveFrozenExportLineage(ctx,who,ready);if err!=nil{return zero,err}
 body=append([]byte(nil),body...)
 if _,err:=DecodeSigningLineage(body,lineage);err!=nil{return zero,err}
 current,err:=s.Catalog.LookupSigningExport(ctx,who,tid,id);if err!=nil{return zero,err}
 if current!=ready{return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return SigningExportEvidence{Project:ready.Project,ExportID:id,Export:ready.Ref,MIME:ready.Object.ContentType,Lineage:lineage,LineageBody:body},nil
}
type SQLSigningExportCatalog struct{DB *pgxpool.Pool;Bucket string}
func(s SQLSigningExportCatalog)LookupSigningExport(ctx context.Context,who auth.Identity,tid,id uuid.UUID)(ReadySigningExport,error){
 var ready ReadySigningExport
 if err:=ctx.Err();err!=nil{return ready,err}
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63||tid==uuid.Nil||id==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return ready,ErrInvalid}
 ready.Tenant=tid;ready.ExportID=id
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  err:=tx.QueryRow(ctx,`SELECT asset.project_id,v.id,t.id,t.run_id,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
   FROM photo.exports e JOIN photo.asset_versions v ON v.tenant_id=e.tenant_id AND v.id=e.asset_version_id
   JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
   JOIN photo.projects p ON p.tenant_id=asset.tenant_id AND p.id=asset.project_id
   JOIN photo.tenants tenant_row ON tenant_row.id=e.tenant_id
   JOIN photo.tasks t ON t.tenant_id=e.tenant_id AND t.id=e.task_id
   JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   JOIN photo.artifacts a ON a.tenant_id=e.tenant_id AND a.id=e.artifact_id
   WHERE e.tenant_id=$1 AND e.id=$2 AND e.status='ready' AND asset.deleted_at IS NULL
   AND p.archived_at IS NULL AND p.download_allowed AND tenant_row.status='active'
   AND t.state='succeeded' AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
   AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
   AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN ('','null')
   AND a.bytes BETWEEN 1 AND 536870912 AND a.media_type IN ('image/jpeg','image/png')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   FOR SHARE OF e,v,asset,p,tenant_row,t,z,a`,tid,id).Scan(&ready.Project,&ready.Version,&ready.Task,&ready.Run,&ready.Ref.ArtifactID,&ready.Ref.SHA256,&bucket,&ready.Object.Key,&ready.Object.VersionID,&ready.Object.Bytes,&ready.Object.ContentType)
  if err!=nil{return err}
  if bucket!=s.Bucket||!ref(ready.Ref)||ready.Object.Key==""{return ErrInvalid}
  // The canonical export query above may wait on publication/lifecycle locks.
  // Never carry a pre-wait Admin role into the project ACL decision.
  currentRole,err:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if err!=nil{return err}
  if _,err:=project.Authorize(ctx,tx,tid,ready.Project,who.UserID,currentRole,domain.Editor,false);err!=nil{return err}
  ready.Object.SHA256=ready.Ref.SHA256;return nil
 })
 if err!=nil{return ReadySigningExport{},err};return ready,nil
}
var _ SigningExportSource=BoundSigningExportSource{}
var _ SigningExportCatalog=SQLSigningExportCatalog{}
