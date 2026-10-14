package interchange
import (
 "bytes"
 "context"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type SQLExportRecipeEvidenceSource struct{DB *pgxpool.Pool;Tenant,Project,Snapshot uuid.UUID;Who auth.Identity;Bucket string;Objects ImportDocumentObjects}
// Read proves both document pins/current scope and exact portable authored bytes.
// It does not prove encoded-image lineage or create a portable recipe artifact.
func(s SQLExportRecipeEvidenceSource)Read(ctx context.Context,item v2input.SessionSnapshotItem,portable v2input.Ref)([]byte,[]byte,error){
 if err:=ctx.Err();err!=nil{return nil,nil,err}
 if s.DB==nil||s.Objects==nil||len(s.Bucket)<3||len(s.Bucket)>63||!validImportRef(portable)||!validImportRef(item.Recipe)||portable.ArtifactID==item.Recipe.ArtifactID{return nil,nil,v2input.ErrInvalid}
 frozenObject,err:=s.lookup(ctx,item,item.Recipe,false);if err!=nil{return nil,nil,err}
 frozenRaw,err:=readExportRecipeObject(ctx,s.Objects,item.Recipe,frozenObject);if err!=nil{return nil,nil,err}
 frozen,err:=v2draft.DecodeFrozenRenderRecipe(frozenRaw);if err!=nil{return nil,nil,err}
 if frozen.Scalars.Source!=item.Binding||frozen.RecipeSHA256!=portable.SHA256||frozen.Learning!=nil{return nil,nil,v2input.ErrInvalid}
 portableObject,err:=s.lookup(ctx,item,portable,true);if err!=nil{return nil,nil,err}
 portableRaw,err:=readExportRecipeObject(ctx,s.Objects,portable,portableObject);if err!=nil{return nil,nil,err}
 if !bytes.Equal(frozen.Recipe,portableRaw)||v2input.ValidateEditingRecipeV3(portableRaw,item.Binding)!=nil{return nil,nil,v2input.ErrInvalid}
 for _,pin:=range []struct{ref v2input.Ref;object objectstore.Object;portable bool}{{item.Recipe,frozenObject,false},{portable,portableObject,true}}{
  current,err:=s.lookup(ctx,item,pin.ref,pin.portable);if err!=nil{return nil,nil,err};if current!=pin.object{return nil,nil,v2input.ErrInvalid}
 }
 if err:=ctx.Err();err!=nil{return nil,nil,err};return frozenRaw,portableRaw,nil
}
func(s SQLExportRecipeEvidenceSource)lookup(ctx context.Context,item v2input.SessionSnapshotItem,ref v2input.Ref,portable bool)(objectstore.Object,error){
 var object objectstore.Object
 err:=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  err:=tx.QueryRow(ctx,`SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type FROM photo.artifacts a WHERE a.tenant_id=$1 AND a.id=$6 AND a.sha256=$7 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND photo.v25_lens_snapshot_ready($1,$2,$3,$4,$5) AND EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=$1 AND p.id=$2 AND p.archived_at IS NULL AND p.download_allowed AND t.status='active') AND EXISTS(SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=$1 AND i.snapshot_id=$3 AND i.ordinal=$8 AND i.asset_version_id=$9 AND i.artifact_id=$10 AND i.sha256=$11) AND CASE WHEN $12 THEN EXISTS(SELECT 1 FROM photo.v2_documents d JOIN photo.v2_document_streams stream ON stream.tenant_id=d.tenant_id AND stream.id=d.stream_id AND stream.project_id=d.project_id WHERE d.tenant_id=$1 AND d.project_id=$2 AND stream.asset_version_id=$9 AND d.document_type='EditingRecipeV3' AND d.artifact_id=a.id AND d.sha256=a.sha256) ELSE photo.v26_render_recipe_scope($1,$2,$9,$6,$7,$4,$5) END FOR SHARE OF a`,s.Tenant,s.Project,s.Snapshot,s.Who.UserID,s.Who.SessionID,ref.ArtifactID,ref.SHA256,item.Ordinal,item.Binding.AssetVersionID,item.Binding.Source.ArtifactID,item.Binding.Source.SHA256,portable).Scan(&bucket,&object.Key,&object.VersionID,&object.SHA256,&object.Bytes,&object.ContentType)
  if err!=nil{return err};if bucket!=s.Bucket||object.Key==""{return objectstore.ErrIntegrity};return nil
 });if err!=nil{return objectstore.Object{},err};return object,nil
}
func readExportRecipeObject(ctx context.Context,objects ImportDocumentObjects,ref v2input.Ref,object objectstore.Object)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if objects==nil||!validImportRef(ref)||object.SHA256!=ref.SHA256||object.Bytes<2||object.Bytes>1<<20||object.ContentType!="application/json"||object.Key==""||object.VersionID==""||object.VersionID=="null"{return nil,v2input.ErrInvalid}
 stream,err:=objects.Open(ctx,object);if err!=nil{if stream!=nil{_ = stream.Close()};return nil,err};if stream==nil{return nil,v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx,stream},object.Bytes+1));closeErr:=stream.Close()
 if err:=ctx.Err();err!=nil{return nil,err};if readErr!=nil{return nil,readErr};if closeErr!=nil{return nil,closeErr}
 if int64(len(raw))!=object.Bytes||rawSHA(raw)!=ref.SHA256{return nil,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return nil,v2input.ErrInvalid};return raw,nil
}

// Resolve selects the immutable portable document by the SHA frozen in the
// recipe, never by stream head. Read independently revalidates the full scope.
func(s SQLExportRecipeEvidenceSource)Resolve(ctx context.Context,item v2input.SessionSnapshotItem)(v2input.Ref,[]byte,[]byte,error){
 var zero v2input.Ref
 if err:=ctx.Err();err!=nil{return zero,nil,nil,err}
 if s.DB==nil||s.Objects==nil||!validImportRef(item.Recipe){return zero,nil,nil,v2input.ErrInvalid}
 object,err:=s.lookup(ctx,item,item.Recipe,false);if err!=nil{return zero,nil,nil,err}
 raw,err:=readExportRecipeObject(ctx,s.Objects,item.Recipe,object);if err!=nil{return zero,nil,nil,err}
 frozen,err:=v2draft.DecodeFrozenRenderRecipe(raw);if err!=nil{return zero,nil,nil,err}
 if frozen.Scalars.Source!=item.Binding||frozen.Learning!=nil{return zero,nil,nil,v2input.ErrInvalid}
 var portable v2input.Ref
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,`SELECT a.id,a.sha256 FROM photo.v2_documents d JOIN photo.v2_document_streams stream ON stream.tenant_id=d.tenant_id AND stream.id=d.stream_id AND stream.project_id=d.project_id JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256 WHERE d.tenant_id=$1 AND d.project_id=$2 AND stream.asset_version_id=$3 AND d.document_type='EditingRecipeV3' AND d.sha256=$4 AND a.id<>$5 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) ORDER BY a.id LIMIT 1 FOR SHARE OF a`,s.Tenant,s.Project,item.Binding.AssetVersionID,frozen.RecipeSHA256,item.Recipe.ArtifactID).Scan(&portable.ArtifactID,&portable.SHA256)
 });if err!=nil{return zero,nil,nil,err}
 frozenRaw,portableRaw,err:=s.Read(ctx,item,portable);if err!=nil{return zero,nil,nil,err}
 if !bytes.Equal(frozenRaw,raw){return zero,nil,nil,v2input.ErrInvalid}
 return portable,frozenRaw,portableRaw,nil
}
