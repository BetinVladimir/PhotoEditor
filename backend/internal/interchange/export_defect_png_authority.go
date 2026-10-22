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

func(s SQLExportDocumentCatalog)QualifyExportDefectPNG(ctx context.Context,parent v2input.Entry,raw []byte,png v2input.Entry)error{return qualifyExportDefectPNG(ctx,s,s,parent,raw,png)}
func(s SQLExportResultInputCatalog)QualifyExportDefectPNG(ctx context.Context,parent v2input.Entry,raw []byte,png v2input.Entry)error{
 config,err:=exportPolicyCatalogConfig(s);if err!=nil{return err};return qualifyExportDefectPNG(ctx,s,config,parent,raw,png)
}
func qualifyExportDefectPNG(ctx context.Context,documents ExportDocumentCatalog,s SQLExportDocumentCatalog,parent v2input.Entry,raw []byte,png v2input.Entry)error{
 if err:=ctx.Err();err!=nil{return err}
 if documents==nil||s.DB==nil||parent.Contract!="DefectProposeReport"||png.Contract!="DefectLabelPNG"||!validImportRef(parent.Ref)||!validImportRef(png.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=parent.Ref.SHA256{return v2input.ErrInvalid}
 var body struct{Proposal v2input.Ref `json:"proposal"`;PNG v2input.Ref `json:"label_png"`;Protection json.RawMessage `json:"protection"`;Params struct{Image v2input.Ref `json:"image"`} `json:"params"`}
 if json.Unmarshal(raw,&body)!=nil||body.PNG!=png.Ref||!validImportRef(body.Proposal)||!validImportRef(body.Params.Image){return v2input.ErrInvalid}
 protection,err:=review.ParseMaskRef(body.Protection);if err!=nil{return err}
 if _,err:=review.ParseProposalProtection(raw,review.ArtifactRef{ID:body.Proposal.ArtifactID,SHA256:body.Proposal.SHA256},review.DefectSource{Image:review.ArtifactRef{ID:body.Params.Image.ArtifactID,SHA256:body.Params.Image.SHA256},Canvas:protection.Canvas,TransformChain:protection.TransformChain});err!=nil{return err}
 parentObject,err:=documents.LookupExportDocument(ctx,parent);if err!=nil{return err}
 pngObject,err:=documents.LookupExportDocument(ctx,png);if err!=nil{return err}
 proposalEntry:=v2input.Entry{Ref:body.Proposal,Contract:"DefectCandidates"}
 proposalObject,err:=documents.LookupExportDocument(ctx,proposalEntry);if err!=nil{return err}
 if pngObject.ContentType!="image/png"||pngObject.Bytes<57{return v2input.ErrInvalid}
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(
   SELECT FROM photo.asset_versions version
   JOIN photo.artifacts report ON report.tenant_id=version.tenant_id AND report.id=$7 AND report.sha256=$8
   JOIN photo.tasks producer ON producer.tenant_id=report.tenant_id AND producer.id=report.created_by_task_id
   JOIN photo.artifacts labels ON labels.tenant_id=producer.tenant_id AND labels.created_by_task_id=producer.id AND labels.output_role='defect_labels'
   CROSS JOIN LATERAL photo.semantic_proposal_defect_label_png_object($1,$2,$3,$4,version.id,$5,$6,$9,$10,$7,$8,labels.id,labels.sha256,$11,$12,producer.params_artifact_id,
    (SELECT params.sha256 FROM photo.artifacts params WHERE params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id),$13,$14) verified
   WHERE version.tenant_id=$1 AND version.master_artifact_id=$5 AND version.content_hash=$6
   AND verified.bucket=$15 AND verified.object_key=$16 AND verified.object_version=$17 AND verified.bytes=$18 AND verified.media_type='image/png'
  )`,s.Tenant,s.Request.ProjectID,s.Who.UserID,s.Who.SessionID,body.Params.Image.ArtifactID,body.Params.Image.SHA256,parent.Ref.ArtifactID,parent.Ref.SHA256,body.Proposal.ArtifactID,body.Proposal.SHA256,protection.Artifact.ID,protection.Artifact.SHA256,png.Ref.ArtifactID,png.Ref.SHA256,s.Bucket,pngObject.Key,pngObject.VersionID,pngObject.Bytes).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2input.ErrInvalid};return ctx.Err()
 })
 if err!=nil{return err}
 for _,check:=range []struct{entry v2input.Entry;objectKey,version,sha string;bytes int64;media string}{
  {parent,parentObject.Key,parentObject.VersionID,parentObject.SHA256,parentObject.Bytes,parentObject.ContentType},
  {png,pngObject.Key,pngObject.VersionID,pngObject.SHA256,pngObject.Bytes,pngObject.ContentType},
  {proposalEntry,proposalObject.Key,proposalObject.VersionID,proposalObject.SHA256,proposalObject.Bytes,proposalObject.ContentType},
 }{current,err:=documents.LookupExportDocument(ctx,check.entry);if err!=nil{return err};if current.Key!=check.objectKey||current.VersionID!=check.version||current.SHA256!=check.sha||current.Bytes!=check.bytes||current.ContentType!=check.media{return v2input.ErrInvalid}}
 return ctx.Err()
}
var _ ExportDefectPNGOccurrenceAuthority=SQLExportDocumentCatalog{}
var _ ExportDefectPNGOccurrenceAuthority=SQLExportResultInputCatalog{}
