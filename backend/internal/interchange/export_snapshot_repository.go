package interchange

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type SQLExportSnapshotObjectRepository struct{DB *pgxpool.Pool;Bucket string}
func(s SQLExportSnapshotObjectRepository)validate(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key string,p CompiledExportSnapshot)(uuid.UUID,error){
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63{return uuid.Nil,v2input.ErrInvalid}
 expected,err:=CompileExportSnapshotRequest(ctx,tid,pid,who,key,p.Request());if err!=nil{return uuid.Nil,err}
 if expected.ID()!=p.ID()||!bytes.Equal(expected.Body(),p.Body()){return uuid.Nil,v2input.ErrInvalid}
 return uuid.NewSHA1(p.ID(),[]byte("interchange:export-snapshot:artifact:1")),nil
}
func(s SQLExportSnapshotObjectRepository)ReserveExportSnapshot(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key string,p CompiledExportSnapshot)(artifactjson.Reservation,error){
 var result artifactjson.Reservation
 id,err:=s.validate(ctx,who,tid,pid,key,p);if err!=nil{return result,err}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var outcome string;var actual,token *uuid.UUID;var ready bool
  if err:=tx.QueryRow(ctx,`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_export_snapshot($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,tid,who.UserID,who.SessionID,pid,key,p.ID(),id,string(p.Request()),string(p.Body()),rawSHA(p.Body()),s.Bucket).Scan(&outcome,&actual,&token,&ready);err!=nil{return err}
  switch outcome{case "quota":return artifactjson.ErrQuota;case "conflict":return artifactjson.ErrConflict;case "created","resumed","replayed":default:return artifactjson.ErrStale}
  if actual==nil||*actual!=id||token==nil||*token==uuid.Nil||ready!=(outcome=="replayed"){return objectstore.ErrIntegrity}
  result=artifactjson.Reservation{ArtifactID:id,Token:*token};if ready{result.Ready=&artifactjson.Ref{ID:id,SHA256:rawSHA(p.Body())}};return nil
 })
 if err!=nil{return artifactjson.Reservation{},err};return result,nil
}
func(s SQLExportSnapshotObjectRepository)CompleteExportSnapshot(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key string,p CompiledExportSnapshot,r artifactjson.Reservation,o objectstore.Object)(v2input.Ref,error){
 var zero v2input.Ref
 id,err:=s.validate(ctx,who,tid,pid,key,p);if err!=nil{return zero,err}
 expectedKey,err:=objectstore.DocumentKey(tid,p.ID(),id);if err!=nil{return zero,err}
 if r.ArtifactID!=id||r.Token==uuid.Nil||o.Key!=expectedKey||o.Bytes!=int64(len(p.Body()))||o.SHA256!=rawSHA(p.Body())||o.ContentType!="application/json"||o.VersionID==""||o.VersionID=="null"||len(o.VersionID)>1024{return zero,objectstore.ErrIntegrity}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var done bool
  if err:=tx.QueryRow(ctx,`SELECT photo.complete_v30_export_snapshot($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,tid,who.UserID,who.SessionID,pid,key,p.ID(),id,r.Token,rawSHA(p.Request()),o.SHA256,s.Bucket,o.VersionID).Scan(&done);err!=nil{return err}
  if !done{return artifactjson.ErrStale};return nil
 })
 if err!=nil{return zero,err};return v2input.Ref{ArtifactID:id,SHA256:o.SHA256},ctx.Err()
}
var _ ExportSnapshotObjectRepository=SQLExportSnapshotObjectRepository{}
