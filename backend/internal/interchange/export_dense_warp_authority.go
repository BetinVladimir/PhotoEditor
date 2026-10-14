package interchange

import (
 "context"
 "encoding/json"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// The occurrence index belongs to the physically pinned parent chain. Fields
// may be reused by later operations, so identical parent producer is not required.
type ExportDenseWarpOccurrenceAuthority interface{
 QualifyExportDenseWarpOccurrence(context.Context,v2input.Entry,[]byte,int,v2input.Entry)error
}
func validateExportDenseWarpOccurrence(parent v2input.Entry,raw []byte,index int,field v2input.Entry)error{
 if !validImportRef(parent.Ref)||!validImportRef(field.Ref)||field.Contract!="DenseWarpField"||index<0||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=parent.Ref.SHA256{return v2input.ErrInvalid}
 var chain json.RawMessage
 switch parent.Contract{
 case "WorkingImage":image,err:=review.ParseWorkingImage(raw);if err!=nil{return err};chain=image.TransformChain
 case "MaskRef":mask,err:=review.ParseMaskRef(raw);if err!=nil{return err};chain=mask.TransformChain
 default:return v2input.ErrInvalid
 }
 var parsed struct{Steps []struct{Kind string `json:"kind"`;Inverse *review.ArtifactRef `json:"inverse_field"`} `json:"steps"`}
 if err:=json.Unmarshal(chain,&parsed);err!=nil{return err}
 if index>=len(parsed.Steps){return v2input.ErrInvalid}
 step:=parsed.Steps[index]
 if step.Kind!="dense_warp"||step.Inverse==nil||step.Inverse.ID!=field.Ref.ArtifactID||step.Inverse.SHA256!=field.Ref.SHA256{return v2input.ErrInvalid}
 return nil
}
func(s SQLExportDocumentCatalog)QualifyExportDenseWarpOccurrence(ctx context.Context,parent v2input.Entry,raw []byte,index int,field v2input.Entry)error{
 if err:=ctx.Err();err!=nil{return err}
 if err:=validateExportDenseWarpOccurrence(parent,raw,index,field);err!=nil{return err}
 if _,err:=s.LookupExportDocument(ctx,parent);err!=nil{return err}
 object,err:=s.LookupExportDocument(ctx,field);if err!=nil{return err}
 if !exportArrayMediaType(object.ContentType){return v2input.ErrInvalid}
 return tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(
   SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   JOIN photo.artifacts f ON f.tenant_id=v.tenant_id AND f.id=$5 AND f.sha256=$6
   WHERE v.tenant_id=$1 AND a.project_id=$2 AND a.deleted_at IS NULL AND a.upload_state='verified'
    AND v.review_state='accepted' AND f.output_role='warp_field'
    AND f.bucket=$10 AND f.object_key=$11 AND f.object_version=$12 AND f.bytes=$13 AND f.media_type=$14
    AND ($7<>'WorkingImage' OR (v.master_artifact_id=$3 AND v.content_hash=$4))
    AND photo.v2_draft_artifact_scope($1,$2,v.id,$3,$4,$7,$8,$9,NULL,NULL)
    AND photo.v2_draft_artifact_scope($1,$2,v.id,$5,$6,'DenseWarpField',$8,$9,$3,$4)
  )`,s.Tenant,s.Request.ProjectID,parent.Ref.ArtifactID,parent.Ref.SHA256,field.Ref.ArtifactID,field.Ref.SHA256,parent.Contract,s.Who.UserID,s.Who.SessionID,s.Bucket,object.Key,object.VersionID,object.Bytes,object.ContentType).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2input.ErrInvalid};return ctx.Err()
 })
}
var _ ExportDenseWarpOccurrenceAuthority=SQLExportDocumentCatalog{}
