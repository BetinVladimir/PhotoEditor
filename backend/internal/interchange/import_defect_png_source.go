package interchange
import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Entry-bound archive discovery. Independent semantic authorities qualify the
// report and proposal before their original refs may identify physical objects.
type ImportArchivedDefectPNGSource struct{
 Entry uuid.UUID
 Parsed []byte
 Reader ImportArchiveDocumentReader
 Catalog ImportDocumentCatalog
 Sources ImportRecipeSourceQualifier
 Reports ImportDefectProposeReportQualifier
 Candidates ImportDefectCandidatesQualifier
 Old,Target v2input.SessionVersionBinding
}
func(s ImportArchivedDefectPNGSource)ResolveExportDefectPNG(ctx context.Context,entry v2input.Entry)([]ExportDefectPNGInput,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Entry==uuid.Nil||s.Reader==nil||s.Catalog==nil||s.Sources==nil||s.Reports==nil||s.Candidates==nil||entry.Contract!="DefectLabelPNG"||!validImportRef(entry.Ref)||!validImportRef(s.Old.Source)||s.Old.AssetVersionID==s.Target.AssetVersionID||s.Old.CanvasHash!=s.Target.CanvasHash{return nil,v2input.ErrInvalid}
 if _,err:=validateParsedImportDocument(ctx,s.Parsed);err!=nil{return nil,err}
 if err:=s.Sources.QualifyImportRecipeSource(ctx,s.Entry,s.Old,s.Target);err!=nil{return nil,err}
 var parsed importParsedDocument;if err:=decodeImportDocument(s.Parsed,&parsed);err!=nil{return nil,err}
 files:=map[string]v2input.Ref{};inventory:=map[uuid.UUID]importBundleInventory{}
 for _,file:=range parsed.Files{files[file.Path]=file.Content}
 for _,item:=range parsed.Manifest.Inventory{inventory[item.ID]=item}
 pngItem,ok:=inventory[entry.Ref.ArtifactID]
 if !ok||pngItem.Contract!="DefectLabelPNG"||pngItem.SHA!=entry.Ref.SHA256||files[pngItem.Path].SHA256!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 read:=func(ref v2input.Ref,kind string)([]byte,error){
  item,ok:=inventory[ref.ArtifactID];if !ok||item.SHA!=ref.SHA256||item.Contract!=kind{return nil,v2input.ErrInvalid}
  staged:=files[item.Path];if !validImportRef(staged)||staged.SHA256!=ref.SHA256{return nil,v2input.ErrInvalid}
  raw,err:=s.Reader.ReadImportArchiveDocument(ctx,staged,1<<20);if err!=nil{return nil,err};if rawSHA(raw)!=ref.SHA256{return nil,v2input.ErrInvalid};return raw,nil
 }
 result:=[]ExportDefectPNGInput{}
 for _,item:=range parsed.Manifest.Inventory{
  if item.Contract!="DefectProposeReport"{continue}
  reportRef:=v2input.Ref{ArtifactID:item.ID,SHA256:item.SHA}
  raw,err:=read(reportRef,item.Contract);if err!=nil{return nil,err}
  var header struct{PNG v2input.Ref `json:"label_png"`;Proposal v2input.Ref `json:"proposal"`}
  if json.Unmarshal(raw,&header)!=nil{return nil,v2input.ErrInvalid}
  if header.PNG.ArtifactID!=entry.Ref.ArtifactID{continue}
  if header.PNG!=entry.Ref{return nil,v2input.ErrInvalid}
  if err:=validateImportDefectProposeReport(raw,s.Old.Source);err!=nil{return nil,err}
  if err:=s.Reports.ValidateArchivedImportDefectProposeReport(ctx,append([]byte(nil),raw...),s.Old);err!=nil{return nil,err}
  proposalRaw,err:=read(header.Proposal,"DefectCandidates");if err!=nil{return nil,err}
  if err:=s.Candidates.ValidateArchivedImportDefectCandidates(ctx,append([]byte(nil),proposalRaw...),s.Old);err!=nil{return nil,err}
  proposal,err:=review.ParseDefectProposalStructure(ctx,proposalRaw,review.ArtifactRef{ID:header.Proposal.ArtifactID,SHA256:header.Proposal.SHA256});if err!=nil{return nil,err}
  binding,err:=proposal.LabelBinding();if err!=nil{return nil,err}
  labels,ok:=inventory[binding.Labels.ID];if !ok||labels.Contract!="DefectLabelMap"||labels.SHA!=binding.Labels.SHA256{return nil,v2input.ErrInvalid}
  staged:=files[labels.Path];if !validImportRef(staged)||staged.SHA256!=labels.SHA{return nil,v2input.ErrInvalid}
  object,err:=s.Catalog.LookupImportDocument(ctx,v2input.Entry{Ref:staged,Contract:"Artifact"});if err!=nil{return nil,err}
  if object.SHA256!=labels.SHA||!exportArrayMediaType(object.ContentType){return nil,v2input.ErrInvalid}
  result=append(result,ExportDefectPNGInput{Report:reportRef,Proposal:proposal,Labels:object})
  if len(result)>128{return nil,v2input.ErrInvalid}
 }
 if len(result)==0{return nil,v2input.ErrInvalid}
 if err:=s.Sources.QualifyImportRecipeSource(ctx,s.Entry,s.Old,s.Target);err!=nil{return nil,err}
 return result,ctx.Err()
}
var _ ExportDefectPNGSource=ImportArchivedDefectPNGSource{}
