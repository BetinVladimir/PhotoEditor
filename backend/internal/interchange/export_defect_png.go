package interchange

import (
 "context"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Each source lookup independently authorizes the report's current original
// producer/attempt/fence and binds PNG, proposal and labels in accepted scope.
type ExportDefectPNGInput struct{
 Report v2input.Ref
 Proposal review.VerifiedProposal
 Labels objectstore.Object
}
type ExportDefectPNGSource interface{ResolveExportDefectPNG(context.Context,v2input.Entry)([]ExportDefectPNGInput,error)}
type ExportDefectPNGQualifier struct{Objects ImportDocumentObjects;Source ExportDefectPNGSource}

func exportDefectPNGBindings(inputs []ExportDefectPNGInput)([]review.DefectLabelBinding,error){
 if len(inputs)<1||len(inputs)>v2input.MaxEntries{return nil,v2input.ErrInvalid}
 result:=make([]review.DefectLabelBinding,len(inputs))
 seen:=map[v2input.Ref]bool{}
 for i,input:=range inputs{
  if !validImportRef(input.Report)||seen[input.Report]{return nil,v2input.ErrInvalid};seen[input.Report]=true
  binding,err:=input.Proposal.LabelBinding();if err!=nil{return nil,err}
  if binding.Width<1||binding.Width>200000000||binding.Height<1||binding.Height>200000000/binding.Width||input.Labels.SHA256!=binding.Labels.SHA256||!exportArrayMediaType(input.Labels.ContentType)||input.Labels.Bytes<1||input.Labels.Bytes>binding.Width*binding.Height*2+exportNPYOverhead||input.Labels.Key==""||input.Labels.VersionID==""||input.Labels.VersionID=="null"{return nil,v2input.ErrInvalid}
  result[i]=binding
 }
 return result,nil
}
func(q ExportDefectPNGQualifier)QualifyExportBinary(ctx context.Context,entry v2input.Entry,object objectstore.Object)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.Objects==nil||q.Source==nil||entry.Contract!="DefectLabelPNG"||!validImportRef(entry.Ref)||object.SHA256!=entry.Ref.SHA256||object.ContentType!="image/png"||object.Bytes<57||object.Key==""||object.VersionID==""||object.VersionID=="null"{return v2input.ErrInvalid}
 inputs,err:=q.Source.ResolveExportDefectPNG(ctx,entry);if err!=nil{return err}
 bindings,err:=exportDefectPNGBindings(inputs);if err!=nil{return err}
 for i,input:=range inputs{
  if err:=ctx.Err();err!=nil{return err}
  binding:=bindings[i];raw:=binding.Height*(binding.Width*2+1)
  if object.Bytes>raw+raw/50+1048576{return v2input.ErrInvalid}
  // Validate candidate inventory separately before comparing its preview.
  labels,err:=q.Objects.Open(ctx,input.Labels);if err!=nil{if labels!=nil{_=labels.Close()};return err};if labels==nil{return v2input.ErrInvalid}
  _,verifyErr:=input.Proposal.VerifyLabelMap(ctx,exportContextReader{ctx,labels},input.Labels.Bytes)
  closeErr:=labels.Close();if verifyErr!=nil{return verifyErr};if closeErr!=nil{return closeErr}
  labels,err=q.Objects.Open(ctx,input.Labels);if err!=nil{if labels!=nil{_=labels.Close()};return err};if labels==nil{return v2input.ErrInvalid}
  png,err:=q.Objects.Open(ctx,object);if err!=nil{_=labels.Close();if png!=nil{_=png.Close()};return err};if png==nil{_=labels.Close();return v2input.ErrInvalid}
  verifyErr=masknpy.VerifyDefectLabelPNG(ctx,masknpy.PinnedMask{Reader:exportContextReader{ctx,labels},Bytes:input.Labels.Bytes,SHA256:input.Labels.SHA256},masknpy.PinnedMask{Reader:exportContextReader{ctx,png},Bytes:object.Bytes,SHA256:object.SHA256},binding.Width,binding.Height)
  leftErr,rightErr:=labels.Close(),png.Close()
  if verifyErr!=nil{return verifyErr};if leftErr!=nil{return leftErr};if rightErr!=nil{return rightErr}
 }
 current,err:=q.Source.ResolveExportDefectPNG(ctx,entry);if err!=nil{return err}
 after,err:=exportDefectPNGBindings(current);if err!=nil{return err}
 if len(after)!=len(bindings){return v2input.ErrInvalid}
 for i:=range inputs{if bindings[i]!=after[i]||inputs[i].Report!=current[i].Report||inputs[i].Labels!=current[i].Labels{return v2input.ErrInvalid}}
 return ctx.Err()
}
var _ ExportBinaryQualifier=ExportDefectPNGQualifier{}
