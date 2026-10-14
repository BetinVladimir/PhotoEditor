package interchange

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// Geometry alone is not producer authority. A native export installation must
// prove the exact parent/array producer relation before probing array bytes.
type ExportWorkingArrayAuthority interface {
 QualifyExportWorkingArray(context.Context,v2input.Entry,v2input.Entry)error
}

func(s SQLExportDocumentCatalog)QualifyExportWorkingArray(ctx context.Context,parent,array v2input.Entry)error{
 if err:=ctx.Err();err!=nil{return err}
 if parent.Contract!="WorkingImage"||(array.Contract!="WorkingPixels"&&array.Contract!="WorkingAlpha")||!validImportRef(parent.Ref)||!validImportRef(array.Ref){return v2input.ErrInvalid}
 // Both objects must remain readable in this pending, frozen export request.
 if _,err:=s.LookupExportDocument(ctx,parent);err!=nil{return err}
 if _,err:=s.LookupExportDocument(ctx,array);err!=nil{return err}
 return tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(
   SELECT 1 FROM photo.asset_versions v JOIN photo.assets a
    ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   WHERE v.tenant_id=$1 AND a.project_id=$2 AND a.deleted_at IS NULL
    AND a.upload_state='verified' AND v.review_state='accepted'
    AND v.master_artifact_id=$3 AND v.content_hash=$4
    AND photo.v2_draft_artifact_scope($1,$2,v.id,$5,$6,$7,$8,$9,$3,$4)
  )`,s.Tenant,s.Request.ProjectID,parent.Ref.ArtifactID,parent.Ref.SHA256,array.Ref.ArtifactID,array.Ref.SHA256,array.Contract,s.Who.UserID,s.Who.SessionID).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2input.ErrInvalid};return ctx.Err()
 })
}
var _ ExportWorkingArrayAuthority=SQLExportDocumentCatalog{}
