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
 "photoeditor/backend/internal/v2input"
)

const exportProposalSnapshotLookupSQL = `SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type,s.item_count FROM photo.snapshots s JOIN photo.artifacts a ON a.tenant_id=s.tenant_id AND a.id=s.artifact_id JOIN photo.projects p ON p.tenant_id=s.tenant_id AND p.id=$2 JOIN photo.tenants t ON t.id=p.tenant_id WHERE s.tenant_id=$1 AND s.id=$3 AND t.status='active' AND p.archived_at IS NULL AND p.download_allowed AND photo.v25_lens_snapshot_ready($1,$2,$3,$4,$5) AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF s,a,p,t`

type ExportProposalFrameFactory interface{
 ExportProposalFrames(context.Context,auth.Identity,uuid.UUID,CreateRequest,string,v2input.Ref,[]byte)(ExportFrameResolver,error)
}
type SQLExportProposalInputs struct{DB *pgxpool.Pool;Objects ImportDocumentObjects;Bucket string;Frames ExportProposalFrameFactory}
func(s SQLExportProposalInputs)ResolveExportProposalInputs(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(v2input.Ref,[]byte,ExportFrameResolver,error){
 var zero v2input.Ref
 if err:=ctx.Err();err!=nil{return zero,nil,nil,err}
 if s.DB==nil||s.Objects==nil||s.Frames==nil||len(s.Bucket)<3||len(s.Bucket)>63{return zero,nil,nil,v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return zero,nil,nil,err}
 if intent.Request.Direction!="export"{return zero,nil,nil,v2input.ErrInvalid}
 ref,object,count,err:=s.lookup(ctx,who,tid,intent.Request,nil);if err!=nil{return zero,nil,nil,err}
 stream,err:=s.Objects.Open(ctx,object);if err!=nil{if stream!=nil{_ = stream.Close()};return zero,nil,nil,err};if stream==nil{return zero,nil,nil,v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(stream,object.Bytes+1));closeErr:=stream.Close()
 if err:=ctx.Err();err!=nil{return zero,nil,nil,err};if readErr!=nil{return zero,nil,nil,readErr};if closeErr!=nil{return zero,nil,nil,closeErr}
 if int64(len(raw))!=object.Bytes||rawSHA(raw)!=ref.SHA256{return zero,nil,nil,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(raw,normalized){return zero,nil,nil,v2input.ErrInvalid}
 snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(ref,raw);if err!=nil{return zero,nil,nil,err}
 if snapshot.SnapshotID!=intent.Identity.Snapshot||len(snapshot.Items)!=count{return zero,nil,nil,v2input.ErrInvalid}
 frames,err:=s.Frames.ExportProposalFrames(ctx,who,tid,copyExportIntent(intent).Request,key,ref,append([]byte(nil),raw...));if err!=nil{return zero,nil,nil,err};if frames==nil{return zero,nil,nil,v2input.ErrInvalid}
 current,currentObject,currentCount,err:=s.lookup(ctx,who,tid,intent.Request,raw);if err!=nil{return zero,nil,nil,err}
 if current!=ref||currentObject!=object||currentCount!=count{return zero,nil,nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,nil,nil,err};return ref,raw,frames,nil
}
func(s SQLExportProposalInputs)lookup(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,raw []byte)(v2input.Ref,objectstore.Object,int,error){
 var ref v2input.Ref;var object objectstore.Object;var count int
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  err:=tx.QueryRow(ctx,exportProposalSnapshotLookupSQL,tid,request.ProjectID,*request.SnapshotID,who.UserID,who.SessionID).Scan(&ref.ArtifactID,&ref.SHA256,&bucket,&object.Key,&object.VersionID,&object.Bytes,&object.ContentType,&count)
  if err!=nil{return err};object.SHA256=ref.SHA256
  if bucket!=s.Bucket||!validImportRef(ref)||object.Key==""{return objectstore.ErrIntegrity}
  if raw!=nil{
   var bound bool
   if err:=tx.QueryRow(ctx,`SELECT NOT EXISTS(SELECT 1 FROM jsonb_array_elements($3::jsonb->'items') node WHERE NOT EXISTS(SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=$1 AND i.snapshot_id=$2 AND i.ordinal=(node->>'ordinal')::integer AND i.asset_version_id=(node#>>'{binding,asset_version_id}')::uuid AND i.artifact_id=(node#>>'{binding,source,artifact_id}')::uuid AND i.sha256=node#>>'{binding,source,sha256}'))`,tid,*request.SnapshotID,string(raw)).Scan(&bound);err!=nil{return err};if !bound{return v2input.ErrInvalid}
  };return nil
 });if err!=nil{return v2input.Ref{},objectstore.Object{},0,err};return ref,object,count,nil
}
var _ ExportProposalInputs=SQLExportProposalInputs{}
