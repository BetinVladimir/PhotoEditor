package interchange

import (
 "context"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Source must return every immutable referring proposal and independently
// authorize its current tenant/project/source/producer scope on each call.
type ExportDefectLabelSource interface {
 ResolveExportDefectLabels(context.Context,v2input.Entry)([]review.VerifiedProposal,error)
}

type ExportDefectLabelQualifier struct {
 Objects ImportDocumentObjects
 Source ExportDefectLabelSource
}

func exportDefectBindings(proposals []review.VerifiedProposal,entry v2input.Entry)([]review.DefectLabelBinding,error) {
 if len(proposals)<1 || len(proposals)>30000 {return nil,v2input.ErrInvalid}
 result:=make([]review.DefectLabelBinding,0,len(proposals))
 seen:=map[review.ArtifactRef]bool{}
 for _,proposal:=range proposals {
  binding,err:=proposal.LabelBinding();if err!=nil {return nil,err}
  if binding.Labels.ID!=entry.Ref.ArtifactID || binding.Labels.SHA256!=entry.Ref.SHA256 ||
   binding.Width<1 || binding.Height<1 || binding.Width>200000000 || binding.Height>200000000 ||
   binding.Width*binding.Height>200000000 || seen[binding.Proposal] {return nil,v2input.ErrInvalid}
  seen[binding.Proposal]=true;result=append(result,binding)
 }
 return result,nil
}

func(q ExportDefectLabelQualifier)QualifyExportBinary(ctx context.Context,entry v2input.Entry,object objectstore.Object)error {
 if err:=ctx.Err();err!=nil {return err}
 if q.Objects==nil || q.Source==nil || entry.Contract!="DefectLabelMap" || !validImportRef(entry.Ref) ||
  object.SHA256!=entry.Ref.SHA256 || !exportArrayMediaType(object.ContentType) || object.Bytes<1 ||
  object.Key=="" || object.VersionID=="" || object.VersionID=="null" {return v2input.ErrInvalid}
 proposals,err:=q.Source.ResolveExportDefectLabels(ctx,entry);if err!=nil {return err}
 bindings,err:=exportDefectBindings(proposals,entry);if err!=nil {return err}
 for i,proposal:=range proposals {
  if err:=ctx.Err();err!=nil {return err}
  if object.Bytes>bindings[i].Width*bindings[i].Height*2+exportNPYOverhead {return v2input.ErrInvalid}
  stream,err:=q.Objects.Open(ctx,object)
  if err!=nil {if stream!=nil {_=stream.Close()};return err}
  if stream==nil {return v2input.ErrInvalid}
  _,verifyErr:=proposal.VerifyLabelMap(ctx,exportContextReader{ctx,stream},object.Bytes)
  closeErr:=stream.Close()
  if verifyErr!=nil {return verifyErr};if closeErr!=nil {return closeErr}
 }
 current,err:=q.Source.ResolveExportDefectLabels(ctx,entry);if err!=nil {return err}
 after,err:=exportDefectBindings(current,entry);if err!=nil {return err}
 if len(after)!=len(bindings) {return v2input.ErrInvalid}
 for i:=range bindings {if bindings[i]!=after[i] {return v2input.ErrInvalid}}
 return ctx.Err()
}
var _ ExportBinaryQualifier=ExportDefectLabelQualifier{}
