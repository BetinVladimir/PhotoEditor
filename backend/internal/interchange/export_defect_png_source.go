package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type ExportDefectPNGOccurrenceAuthority interface{QualifyExportDefectPNG(context.Context,v2input.Entry,[]byte,v2input.Entry)error}
type ExportTypedDefectPNGSource struct{
 Reader ExportClosureNodeResolver
 Catalog ExportDocumentCatalog
 Reports []v2input.Entry
 Authority ExportDefectPNGOccurrenceAuthority
}
func(s ExportTypedDefectPNGSource)ResolveExportDefectPNG(ctx context.Context,entry v2input.Entry)([]ExportDefectPNGInput,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Reader==nil||s.Catalog==nil||s.Authority==nil||entry.Contract!="DefectLabelPNG"||!validImportRef(entry.Ref)||len(s.Reports)<1||len(s.Reports)>v2input.MaxEntries{return nil,v2input.ErrInvalid}
 result:=[]ExportDefectPNGInput{};seen:=map[uuid.UUID]bool{}
 for _,parent:=range append([]v2input.Entry(nil),s.Reports...){
  if parent.Contract!="DefectProposeReport"||!validImportRef(parent.Ref)||seen[parent.Ref.ArtifactID]{return nil,v2input.ErrInvalid};seen[parent.Ref.ArtifactID]=true
  node,err:=s.Reader.ResolveExportNode(ctx,parent);if err!=nil{return nil,err}
  if node.Entry!=parent||rawSHA(node.Body)!=parent.Ref.SHA256{return nil,v2input.ErrInvalid}
  var report struct{Proposal v2input.Ref `json:"proposal"`;PNG v2input.Ref `json:"label_png"`;Params struct{Image v2input.Ref `json:"image"`} `json:"params"`}
  if json.Unmarshal(node.Body,&report)!=nil{return nil,v2input.ErrInvalid}
  if report.PNG.ArtifactID!=entry.Ref.ArtifactID{continue}
  if report.PNG!=entry.Ref||!validImportRef(report.Proposal)||!validImportRef(report.Params.Image){return nil,v2input.ErrInvalid}
  expected,err:=v2draft.DocumentChildren(node.Body,parent.Contract,parent.Ref);if err!=nil{return nil,err}
  for _,required:=range []v2input.Entry{entry,{Ref:report.Proposal,Contract:"DefectCandidates"},{Ref:report.Params.Image,Contract:"WorkingImage"}}{
   declared,loaded:=false,false
   for _,ref:=range expected{if ref.Ref==required.Ref&&ref.Contract==required.Contract{declared=true}}
   for _,child:=range node.Children{if child.Ref.ArtifactID==required.Ref.ArtifactID{if child!=required{return nil,v2input.ErrInvalid};loaded=true}}
   if !declared||!loaded{return nil,v2input.ErrInvalid}
  }
  if err:=s.Authority.QualifyExportDefectPNG(ctx,parent,append([]byte(nil),node.Body...),entry);err!=nil{return nil,err}
  proposalEntry:=v2input.Entry{Ref:report.Proposal,Contract:"DefectCandidates"}
  proposalNode,err:=s.Reader.ResolveExportNode(ctx,proposalEntry);if err!=nil{return nil,err}
  if proposalNode.Entry!=proposalEntry||rawSHA(proposalNode.Body)!=report.Proposal.SHA256{return nil,v2input.ErrInvalid}
  imageEntry:=v2input.Entry{Ref:report.Params.Image,Contract:"WorkingImage"}
  imageNode,err:=s.Reader.ResolveExportNode(ctx,imageEntry);if err!=nil{return nil,err}
  if imageNode.Entry!=imageEntry||rawSHA(imageNode.Body)!=report.Params.Image.SHA256{return nil,v2input.ErrInvalid}
  image,err:=review.ParseWorkingImage(imageNode.Body);if err!=nil{return nil,err}
  source:=image.Source(review.ArtifactRef{ID:report.Params.Image.ArtifactID,SHA256:report.Params.Image.SHA256})
  if _,err:=review.ParseProposalProtection(node.Body,review.ArtifactRef{ID:report.Proposal.ArtifactID,SHA256:report.Proposal.SHA256},source);err!=nil{return nil,err}
  proposal,err:=review.ReadDefectProposal(ctx,bytes.NewReader(proposalNode.Body),review.ArtifactRef{ID:report.Proposal.ArtifactID,SHA256:report.Proposal.SHA256},source);if err!=nil{return nil,err}
  binding,err:=proposal.LabelBinding();if err!=nil{return nil,err}
  labelEntry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:binding.Labels.ID,SHA256:binding.Labels.SHA256},Contract:"DefectLabelMap"}
  found:=false;for _,child:=range proposalNode.Children{if child.Ref.ArtifactID==labelEntry.Ref.ArtifactID{if child!=labelEntry{return nil,v2input.ErrInvalid};found=true}}
  if !found{return nil,v2input.ErrInvalid}
  labelAuthority,ok:=s.Catalog.(ExportDefectLabelOccurrenceAuthority);if !ok{return nil,v2input.ErrInvalid}
  if err:=labelAuthority.QualifyExportDefectLabel(ctx,proposalEntry,append([]byte(nil),proposalNode.Body...),labelEntry);err!=nil{return nil,err}
  labels,err:=s.Catalog.LookupExportDocument(ctx,labelEntry);if err!=nil{return nil,err}
  if err:=s.Authority.QualifyExportDefectPNG(ctx,parent,append([]byte(nil),node.Body...),entry);err!=nil{return nil,err}
  result=append(result,ExportDefectPNGInput{Report:parent.Ref,Proposal:proposal,Labels:labels})
 }
 if len(result)==0{return nil,v2input.ErrInvalid}
 return result,ctx.Err()
}
var _ ExportDefectPNGSource=ExportTypedDefectPNGSource{}
