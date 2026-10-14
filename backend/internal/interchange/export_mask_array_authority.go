package interchange

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type ExportMaskArrayAuthority interface{
 QualifyExportMaskArray(context.Context,v2input.Entry,[]byte,v2input.Entry)error
}
func(s SQLExportDocumentCatalog)QualifyExportMaskArray(ctx context.Context,parent v2input.Entry,raw []byte,array v2input.Entry)error{
 if err:=ctx.Err();err!=nil{return err}
 if parent.Contract!="MaskRef"||array.Contract!="MaskPixels"||!validImportRef(parent.Ref)||!validImportRef(array.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=parent.Ref.SHA256{return v2input.ErrInvalid}
 mask,err:=review.ParseMaskRef(raw);if err!=nil{return err}
 if mask.Artifact.ID!=array.Ref.ArtifactID||mask.Artifact.SHA256!=array.Ref.SHA256{return v2input.ErrInvalid}
 if _,err:=s.LookupExportDocument(ctx,parent);err!=nil{return err}
 object,err:=s.LookupExportDocument(ctx,array);if err!=nil{return err}
 if !exportArrayMediaType(object.ContentType){return v2input.ErrInvalid}
 return tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(
   SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   JOIN photo.artifacts pixels ON pixels.tenant_id=v.tenant_id AND pixels.id=$5 AND pixels.sha256=$6
   WHERE v.tenant_id=$1 AND a.project_id=$2 AND a.deleted_at IS NULL AND a.upload_state='verified'
    AND v.review_state='accepted' AND pixels.output_role IN('mask_pixels','mask_target','mask_protected')
    AND pixels.bucket=$9 AND pixels.object_key=$10 AND pixels.object_version=$11 AND pixels.bytes=$12 AND pixels.media_type=$13
    AND photo.v2_draft_artifact_scope($1,$2,v.id,$3,$4,'MaskRef',$7,$8,NULL,NULL)
    AND photo.v2_draft_artifact_scope($1,$2,v.id,$5,$6,'MaskPixels',$7,$8,$3,$4)
  )`,s.Tenant,s.Request.ProjectID,parent.Ref.ArtifactID,parent.Ref.SHA256,array.Ref.ArtifactID,array.Ref.SHA256,s.Who.UserID,s.Who.SessionID,s.Bucket,object.Key,object.VersionID,object.Bytes,object.ContentType).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2input.ErrInvalid};return ctx.Err()
 })
}
var _ ExportMaskArrayAuthority=SQLExportDocumentCatalog{}
