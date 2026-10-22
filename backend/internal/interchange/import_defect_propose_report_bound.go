package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Pins the semantic/physical report verifier to one accepted import entry.
// This wrapper supplies archive identity and prepared proposal/source evidence;
// Semantic remains responsible for producer ancestry and PNG/label equality.
type BoundImportDefectProposeReportQualifier struct{
 Entry uuid.UUID
 Old,Target v2input.SessionVersionBinding
 Parsed []byte
 Reader ImportArchiveDocumentReader
 Documents ImportPreparedDocumentReader
 Masters ImportMasterReader
 Sources ImportRecipeSourceQualifier
 Candidates ImportDefectCandidatesQualifier
 Semantic ImportDefectProposeReportQualifier
}
func(q BoundImportDefectProposeReportQualifier)validate(ctx context.Context)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.Entry==uuid.Nil||q.Reader==nil||q.Documents==nil||q.Masters==nil||q.Sources==nil||q.Candidates==nil||q.Semantic==nil||!validImportRef(q.Old.Source)||!validImportRef(q.Target.Source)||q.Old.AssetVersionID==uuid.Nil||q.Target.AssetVersionID==uuid.Nil||q.Old.AssetVersionID==q.Target.AssetVersionID||q.Old.CanvasHash!=q.Target.CanvasHash{return v2input.ErrInvalid}
 _,err:=validateParsedImportDocument(ctx,q.Parsed);return err
}
func(q BoundImportDefectProposeReportQualifier)ValidateArchivedImportDefectProposeReport(ctx context.Context,raw []byte,old v2input.SessionVersionBinding)error{
 if err:=q.validate(ctx);err!=nil{return err}
 if old!=q.Old{return v2input.ErrInvalid}
 if err:=validateImportDefectProposeReport(raw,old.Source);err!=nil{return err}
 var parsed importParsedDocument
 if err:=decodeImportDocument(q.Parsed,&parsed);err!=nil{return err}
 files:=map[string]v2input.Ref{}
 for _,file:=range parsed.Files{files[file.Path]=file.Content}
 matched:=false
 for _,item:=range parsed.Manifest.Inventory{
  if item.Contract!="DefectProposeReport"||item.SHA!=rawSHA(raw){continue}
  staged:=files[item.Path];if !validImportRef(staged)||staged.SHA256!=item.SHA{return v2input.ErrInvalid}
  current,err:=q.Reader.ReadImportArchiveDocument(ctx,staged,1<<20);if err!=nil{return err}
  if !bytes.Equal(raw,current){return v2input.ErrInvalid};matched=true
 }
 if !matched{return v2input.ErrInvalid}
 if err:=q.Sources.QualifyImportRecipeSource(ctx,q.Entry,q.Old,q.Target);err!=nil{return err}
 if err:=q.Semantic.ValidateArchivedImportDefectProposeReport(ctx,append([]byte(nil),raw...),old);err!=nil{return err}
 return q.Sources.QualifyImportRecipeSource(ctx,q.Entry,q.Old,q.Target)
}
func(q BoundImportDefectProposeReportQualifier)QualifyImportedDefectProposeReport(ctx context.Context,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=q.validate(ctx);err!=nil{return err}
 if target!=q.Target{return v2input.ErrInvalid}
 if err:=validateImportDefectProposeReport(raw,target.Source);err!=nil{return err}
 if err:=q.Sources.QualifyImportRecipeSource(ctx,q.Entry,q.Old,q.Target);err!=nil{return err}
 var header struct{Proposal v2input.Ref `json:"proposal"`}
 if json.Unmarshal(raw,&header)!=nil||!validImportRef(header.Proposal){return v2input.ErrInvalid}
 entry:=v2input.Entry{Ref:header.Proposal,Contract:"DefectCandidates"}
 proposal,err:=q.Documents.ReadPreparedImportDocument(ctx,entry,1<<20);if err!=nil{return err}
 if rawSHA(proposal)!=header.Proposal.SHA256{return v2input.ErrInvalid}
 if err:=q.Candidates.QualifyImportedDefectCandidates(ctx,proposal,target);err!=nil{return err}
 master,err:=q.Masters.ReadImportMaster(ctx,target.Source,1<<20);if err!=nil{return err}
 image,err:=review.ParseWorkingImage(master);if err!=nil||rawSHA(master)!=target.Source.SHA256{return v2input.ErrInvalid}
 source:=image.Source(review.ArtifactRef{ID:target.Source.ArtifactID,SHA256:target.Source.SHA256})
 if _,err:=review.ParseProposalProtection(raw,review.ArtifactRef{ID:header.Proposal.ArtifactID,SHA256:header.Proposal.SHA256},source);err!=nil{return err}
 if err:=q.Semantic.QualifyImportedDefectProposeReport(ctx,append([]byte(nil),raw...),target);err!=nil{return err}
 current,err:=q.Documents.ReadPreparedImportDocument(ctx,entry,1<<20);if err!=nil{return err};if !bytes.Equal(current,proposal){return v2input.ErrInvalid}
 current,err=q.Masters.ReadImportMaster(ctx,target.Source,1<<20);if err!=nil{return err};if !bytes.Equal(current,master){return v2input.ErrInvalid}
 if err:=q.Candidates.QualifyImportedDefectCandidates(ctx,proposal,target);err!=nil{return err}
 return q.Sources.QualifyImportRecipeSource(ctx,q.Entry,q.Old,q.Target)
}
var _ ImportDefectProposeReportQualifier=BoundImportDefectProposeReportQualifier{}
