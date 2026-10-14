package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type ExportDefectLabelOccurrenceAuthority interface {
 QualifyExportDefectLabel(context.Context,v2input.Entry,[]byte,v2input.Entry)error
}

type ExportTypedDefectLabelSource struct {
 Reader ExportClosureNodeResolver
 Proposals []v2input.Entry
 Authority ExportDefectLabelOccurrenceAuthority
}
func(s ExportTypedDefectLabelSource)ResolveExportDefectLabels(ctx context.Context,entry v2input.Entry)([]review.VerifiedProposal,error) {
 if err:=ctx.Err();err!=nil {return nil,err}
 if s.Reader==nil||s.Authority==nil||entry.Contract!="DefectLabelMap"||!validImportRef(entry.Ref)||len(s.Proposals)<1||len(s.Proposals)>v2input.MaxEntries {return nil,v2input.ErrInvalid}
 result:=[]review.VerifiedProposal{};seen:=map[uuid.UUID]bool{}
 for _,parent:=range append([]v2input.Entry(nil),s.Proposals...) {
  if err:=ctx.Err();err!=nil {return nil,err}
  if parent.Contract!="DefectCandidates"||!validImportRef(parent.Ref)||seen[parent.Ref.ArtifactID] {return nil,v2input.ErrInvalid}
  seen[parent.Ref.ArtifactID]=true
  node,err:=s.Reader.ResolveExportNode(ctx,parent);if err!=nil {return nil,err}
  if node.Entry!=parent||rawSHA(node.Body)!=parent.Ref.SHA256 {return nil,v2input.ErrInvalid}
  var projection struct {Image v2input.Ref `json:"image"`;Labels v2input.Ref `json:"label_map"`}
  if json.Unmarshal(node.Body,&projection)!=nil {return nil,v2input.ErrInvalid}
  if projection.Labels.ArtifactID!=entry.Ref.ArtifactID {continue}
  if projection.Labels!=entry.Ref||!validImportRef(projection.Image) {return nil,v2input.ErrInvalid}
  labelFound,imageFound:=false,false
  imageEntry:=v2input.Entry{Ref:projection.Image,Contract:"WorkingImage"}
  for _,child:=range node.Children {
   if child.Ref.ArtifactID==entry.Ref.ArtifactID {if child!=entry {return nil,v2input.ErrInvalid};labelFound=true}
   if child.Ref.ArtifactID==projection.Image.ArtifactID {if child!=imageEntry {return nil,v2input.ErrInvalid};imageFound=true}
  }
  if !labelFound||!imageFound {return nil,v2input.ErrInvalid}
  imageNode,err:=s.Reader.ResolveExportNode(ctx,imageEntry);if err!=nil {return nil,err}
  if imageNode.Entry!=imageEntry||rawSHA(imageNode.Body)!=projection.Image.SHA256 {return nil,v2input.ErrInvalid}
  image,err:=review.ParseWorkingImage(imageNode.Body);if err!=nil {return nil,err}
  verified,err:=review.ReadDefectProposal(ctx,bytes.NewReader(node.Body),review.ArtifactRef{ID:parent.Ref.ArtifactID,SHA256:parent.Ref.SHA256},
   image.Source(review.ArtifactRef{ID:projection.Image.ArtifactID,SHA256:projection.Image.SHA256}));if err!=nil {return nil,err}
  if err:=s.Authority.QualifyExportDefectLabel(ctx,parent,append([]byte(nil),node.Body...),entry);err!=nil {return nil,err}
  result=append(result,verified)
 }
 if len(result)==0 {return nil,v2input.ErrInvalid}
 return result,ctx.Err()
}
var _ ExportDefectLabelSource=ExportTypedDefectLabelSource{}
