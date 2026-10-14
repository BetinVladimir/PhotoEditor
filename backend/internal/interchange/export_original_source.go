package interchange
import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type ExportEncodedFrame struct{Ref v2input.Ref;Path string;MediaType string}
type SQLExportOriginalSource struct{DB *pgxpool.Pool;Tenant,Project,Snapshot uuid.UUID;Who auth.Identity;Bucket string;Objects ImportDocumentObjects;Native ExportBinaryQualifier;MaxBytes int64}
func exportOriginalSuffix(media string)(string,error){switch media{case "image/tiff":return ".tiff",nil;case "image/jpeg":return ".jpg",nil;case "image/png":return ".png",nil};return "",v2input.ErrInvalid}
// This source covers already encoded originals. RAW conversion and replayed
// encoded outputs require a separate qualified producer; MIME is never renamed.
func(s SQLExportOriginalSource)Read(ctx context.Context,item v2input.SessionSnapshotItem)(ExportEncodedFrame,error){
 var zero ExportEncodedFrame
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||s.Objects==nil||s.Native==nil||len(s.Bucket)<3||len(s.Bucket)>63||s.MaxBytes<1||s.MaxBytes>64<<30||item.Binding.AssetVersionID==uuid.Nil||!validImportRef(item.Binding.Source){return zero,v2input.ErrInvalid}
 ref,object,err:=s.lookup(ctx,item);if err!=nil{return zero,err}
 suffix,err:=exportOriginalSuffix(object.ContentType);if err!=nil{return zero,err}
 catalog:=exportOriginalCatalog{source:s,item:item,expected:ref}
 if _,err:=(ExportBinaryNodeResolver{Catalog:catalog,Objects:s.Objects,Qualifier:s.Native,MaxBytes:s.MaxBytes}).ResolveExportNode(ctx,v2input.Entry{Ref:ref,Contract:"EncodedImage"});err!=nil{return zero,err}
 current,currentObject,err:=s.lookup(ctx,item);if err!=nil{return zero,err}
 if current!=ref||currentObject!=object{return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return ExportEncodedFrame{Ref:ref,Path:"images/"+item.Binding.AssetVersionID.String()+suffix,MediaType:object.ContentType},nil
}
type exportOriginalCatalog struct{source SQLExportOriginalSource;item v2input.SessionSnapshotItem;expected v2input.Ref}
func(c exportOriginalCatalog)LookupExportDocument(ctx context.Context,entry v2input.Entry)(objectstore.Object,error){
 if entry.Contract!="EncodedImage"||entry.Ref!=c.expected{return objectstore.Object{},v2input.ErrInvalid}
 ref,object,err:=c.source.lookup(ctx,c.item);if err!=nil{return objectstore.Object{},err};if ref!=c.expected{return objectstore.Object{},v2input.ErrInvalid};return object,nil
}
func(s SQLExportOriginalSource)lookup(ctx context.Context,item v2input.SessionSnapshotItem)(v2input.Ref,objectstore.Object,error){
 var ref v2input.Ref;var object objectstore.Object
 err:=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  err:=tx.QueryRow(ctx,`SELECT original.id,original.sha256,original.bucket,original.object_key,original.object_version,original.bytes,original.media_type FROM photo.asset_versions v JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND original.id=asset.original_artifact_id WHERE v.tenant_id=$1 AND v.id=$2 AND v.master_artifact_id=$3 AND v.content_hash=$4 AND asset.project_id=$5 AND asset.deleted_at IS NULL AND asset.upload_state='verified' AND original.state='ready' AND original.verified_at IS NOT NULL AND original.object_version IS NOT NULL AND original.object_version NOT IN('','null') AND original.bytes BETWEEN 1 AND $6 AND original.media_type IN('image/tiff','image/jpeg','image/png') AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp()) AND photo.v25_lens_snapshot_ready($1,$5,$7,$8,$9) AND EXISTS(SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=$1 AND i.snapshot_id=$7 AND i.ordinal=$10 AND i.asset_version_id=v.id AND i.artifact_id=v.master_artifact_id AND i.sha256=v.content_hash) AND EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=$1 AND p.id=$5 AND p.archived_at IS NULL AND p.download_allowed AND t.status='active') FOR SHARE OF v,asset,original`,s.Tenant,item.Binding.AssetVersionID,item.Binding.Source.ArtifactID,item.Binding.Source.SHA256,s.Project,s.MaxBytes,s.Snapshot,s.Who.UserID,s.Who.SessionID,item.Ordinal).Scan(&ref.ArtifactID,&ref.SHA256,&bucket,&object.Key,&object.VersionID,&object.Bytes,&object.ContentType)
  if err!=nil{return err};object.SHA256=ref.SHA256;if bucket!=s.Bucket||!validImportRef(ref)||object.Key==""{return objectstore.ErrIntegrity};return nil
 });if err!=nil{return v2input.Ref{},objectstore.Object{},err};return ref,object,nil
}
