package collection

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
)

// Counts describe verified upload rows and consumed download authorizations, not
// unverifiable browser transfer completion. No secret or guest identity leaks.
type ShareUsage struct {
 ShareID uuid.UUID `json:"share_id"`
 UploadedFiles string `json:"uploaded_files"`
 UploadedBytes string `json:"uploaded_bytes"`
 QuotaUsedFiles string `json:"quota_used_files"`
 QuotaUsedBytes string `json:"quota_used_bytes"`
 ReservedFiles string `json:"reserved_files"`
 ReservedBytes string `json:"reserved_bytes"`
 DownloadAuthorizations string `json:"download_authorizations"`
 DownloadJobs string `json:"download_jobs"`
}
func (s Service) ShareUsage(ctx context.Context,who auth.Identity,tid,collectionID,shareID uuid.UUID)(ShareUsage,error){
 var out ShareUsage
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if err:=currentEditorProject(ctx,tx,who,tid,collectionID,role);err!=nil{return err}
  err:=tx.QueryRow(ctx,`SELECT l.id,
   (SELECT count(*)::text FROM photo.upload_sessions u WHERE u.tenant_id=l.tenant_id AND u.collection_id=l.collection_id AND u.share_id=l.id AND u.principal_type='guest' AND u.state='verified'),
   (SELECT coalesce(sum(u.actual_size),0)::text FROM photo.upload_sessions u WHERE u.tenant_id=l.tenant_id AND u.collection_id=l.collection_id AND u.share_id=l.id AND u.principal_type='guest' AND u.state='verified'),
   l.used_files::text,l.used_bytes::text,l.reserved_files::text,l.reserved_bytes::text,
   (SELECT count(*)::text FROM photo.download_intents i WHERE i.tenant_id=l.tenant_id AND i.share_id=l.id AND i.consumed_at IS NOT NULL),
   (SELECT count(*)::text FROM photo.download_jobs j WHERE j.tenant_id=l.tenant_id AND j.share_id=l.id)
   FROM photo.share_links l WHERE l.tenant_id=$1 AND l.collection_id=$2 AND l.id=$3`,tid,collectionID,shareID).Scan(&out.ShareID,&out.UploadedFiles,&out.UploadedBytes,&out.QuotaUsedFiles,&out.QuotaUsedBytes,&out.ReservedFiles,&out.ReservedBytes,&out.DownloadAuthorizations,&out.DownloadJobs)
  if err==pgx.ErrNoRows{return domain.NewError("NOT_FOUND",404)};return err
 });return out,err
}
