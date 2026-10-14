package interchange

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// Bound to one current review and entry. This does not approve a document or
// authorize an unselected version; native scope additionally re-reads its master.
type SQLImportReviewTargetAuthority struct{Catalog SQLImportApprovalCatalog}
func(a SQLImportReviewTargetAuthority)QualifyImportBackgroundTarget(ctx context.Context,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 s:=a.Catalog
 if s.DB==nil||s.Scope.Tenant==uuid.Nil||s.Scope.Workflow==uuid.Nil||s.Scope.Run==uuid.Nil||s.Scope.Entry==uuid.Nil||s.Who.UserID==uuid.Nil||s.Who.SessionID==uuid.Nil||s.Revision<1||s.Revision>9007199254740990||binding!=s.Scope.Binding||binding.AssetVersionID==uuid.Nil||!validImportRef(binding.Source)||!validImportRef(v2input.Ref{ArtifactID:s.Scope.Entry,SHA256:binding.CanvasHash})||!validImportRef(v2input.Ref{ArtifactID:s.Scope.Entry,SHA256:s.ReportSHA}){return v2input.ErrInvalid}
 var allowed bool
 err:=tenant.WithTenant(ctx,s.DB,s.Who,s.Scope.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,`WITH current_review AS (
   SELECT photo.v30_interchange_review_context($1,$2,$3,$4,$5,$6) AS body)
   SELECT EXISTS(SELECT 1 FROM current_review c
   CROSS JOIN LATERAL jsonb_array_elements(c.body#>'{review,targets}') target
   JOIN photo.asset_versions v ON v.tenant_id=$1 AND v.id=(target->>'asset_version_id')::uuid
   JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id AND asset.deleted_at IS NULL
   JOIN photo.v2_document_streams stream ON stream.tenant_id=v.tenant_id AND stream.id=(target->>'stream_id')::uuid
   JOIN photo.v2_documents document ON document.tenant_id=stream.tenant_id AND document.id=stream.current_document_id
   WHERE c.body->>'run_id'=$7::text AND target->>'entry_id'=$8::text AND target->>'state'='proposed'
   AND v.id=$9 AND v.master_artifact_id=$10 AND v.content_hash=$11
   AND asset.project_id=(c.body->>'project_id')::uuid
   AND stream.project_id=asset.project_id AND stream.asset_version_id=v.id AND stream.stream_type='draft'
   AND stream.current_document_id=(target->>'document_id')::uuid AND stream.approved_document_id IS NULL
   AND document.project_id=stream.project_id AND document.stream_id=stream.id
   AND document.artifact_id=(target#>>'{recipe,artifact_id}')::uuid AND document.sha256=target#>>'{recipe,sha256}')`,
   s.Scope.Tenant,s.Who.UserID,s.Who.SessionID,s.Scope.Workflow,s.Revision,s.ReportSHA,s.Scope.Run,s.Scope.Entry,binding.AssetVersionID,binding.Source.ArtifactID,binding.Source.SHA256).Scan(&allowed)
 });if err!=nil{return err}
 if !allowed{return v2input.ErrInvalid}
 return ctx.Err()
}
var _ ImportBackgroundTargetAuthority=SQLImportReviewTargetAuthority{}
