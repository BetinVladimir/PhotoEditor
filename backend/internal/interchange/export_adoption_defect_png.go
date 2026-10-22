package interchange
import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func(s SQLSchedulerExportInputCatalog)QualifyExportDefectPNG(ctx context.Context,parent v2input.Entry,raw []byte,png v2input.Entry)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.Claims.DB==nil||s.Claim.validate()!=nil||parent.Contract!="DefectProposeReport"||png.Contract!="DefectLabelPNG"||!validImportRef(parent.Ref)||!validImportRef(png.Ref)||rawSHA(raw)!=parent.Ref.SHA256{return v2input.ErrInvalid}
 if _,err:=exportReportInlineMasks(parent,raw);err!=nil{return err}
 var body struct{PNG v2input.Ref `json:"label_png"`;Proposal v2input.Ref `json:"proposal"`;Protection json.RawMessage `json:"protection"`;Params struct{Image v2input.Ref `json:"image"`} `json:"params"`}
 if json.Unmarshal(raw,&body)!=nil||body.PNG!=png.Ref||!validImportRef(body.Proposal)||!validImportRef(body.Params.Image){return v2input.ErrInvalid}
 protection,err:=review.ParseMaskRef(body.Protection);if err!=nil{return err}
 entries:=[]v2input.Entry{parent,png,{Ref:body.Proposal,Contract:"DefectCandidates"},{Ref:body.Params.Image,Contract:"WorkingImage"},{Ref:v2input.Ref{ArtifactID:protection.Artifact.ID,SHA256:protection.Artifact.SHA256},Contract:"MaskPixels"}}
 // Exact before/after catalog reads repeat current export claim admission.
 before:=make([]objectstore.Object,len(entries))
 for i,entry:=range entries{object,err:=s.LookupExportDocument(ctx,entry);if err!=nil{return err};before[i]=object}
 var allowed bool
 err=s.Claims.DB.QueryRow(ctx,`SELECT photo.v30_export_adoption_defect_png($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,s.Claim.Tenant,s.Claim.Workflow,s.Claim.Run,s.Claim.Task,s.Claim.Result,s.Claim.Actor,s.Claim.Key,s.Claim.RequestSHA,s.Claim.Token,body.Params.Image.ArtifactID,body.Params.Image.SHA256,body.Proposal.ArtifactID,body.Proposal.SHA256,parent.Ref.ArtifactID,parent.Ref.SHA256,protection.Artifact.ID,protection.Artifact.SHA256,png.Ref.ArtifactID,png.Ref.SHA256).Scan(&allowed)
 if err!=nil{return err};if !allowed{return v2input.ErrInvalid}
 for i,entry:=range entries{object,err:=s.LookupExportDocument(ctx,entry);if err!=nil{return err};if object!=before[i]{return v2input.ErrInvalid}}
 return ctx.Err()
}
func(s SQLSchedulerExportInputCatalog)QualifyExportReportMask(ctx context.Context,parent v2input.Entry,raw []byte,mask v2input.Ref)error{return qualifyExportReportMask(ctx,s,s,parent,raw,mask)}
var _ ExportDefectPNGOccurrenceAuthority=SQLSchedulerExportInputCatalog{}
var _ ExportReportMaskAuthority=SQLSchedulerExportInputCatalog{}
