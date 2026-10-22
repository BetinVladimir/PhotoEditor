package interchange

import (
 "context"
 "encoding/json"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Occurrence membership supplies all parents. Every prepared document still
// passes independent semantic authority before physical PNG/label comparison.
type SQLPreparedDefectPNGSource struct {
 Catalog SQLPreparedImportCatalog
 Documents ImportClosureNodeResolver
 LabelWitnesses *ImportPreparedDefectLabelWitnesses
 Labels ExportDefectLabelSource
}
func(s SQLPreparedDefectPNGSource)ResolveExportDefectPNG(ctx context.Context,entry v2input.Entry)([]ExportDefectPNGInput,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if entry.Contract!="DefectLabelPNG"||!validImportRef(entry.Ref)||s.Catalog.Store.DB==nil||s.Documents==nil||s.LabelWitnesses==nil||s.Labels==nil{return nil,v2input.ErrInvalid}
 before,err:=s.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return nil,err}
 scope,store:=s.Catalog.Scope,s.Catalog.Store
 binding,err:=canonical.Marshal(scope.Binding);if err!=nil{return nil,err}
 var raw []byte
 err=database.TenantTransaction(ctx,store.DB,scope.Tenant,func(tx pgx.Tx)error{
  if err:=store.checkPreparedRun(ctx,tx);err!=nil{return err}
  return tx.QueryRow(ctx,`SELECT photo.v30_interchange_prepared_defect_png_parents($1,$2,$3,$4,$5::jsonb,$6,$7)`,scope.Tenant,scope.Workflow,string(store.Parsed),scope.Entry,string(binding),entry.Ref.ArtifactID,entry.Ref.SHA256).Scan(&raw)
 });if err!=nil{return nil,err}
 var parents []v2input.Ref;if decodeImportDocument(raw,&parents)!=nil||len(parents)<1||len(parents)>128{return nil,v2input.ErrInvalid}
 result:=make([]ExportDefectPNGInput,0,len(parents));seen:=map[v2input.Ref]bool{}
 for _,parent:=range parents{
  if !validImportRef(parent)||seen[parent]{return nil,v2input.ErrInvalid};seen[parent]=true
  reportEntry:=v2input.Entry{Ref:parent,Contract:"DefectProposeReport"}
  node,err:=s.Documents.ResolveImportNode(ctx,reportEntry);if err!=nil{return nil,err}
  if node.Entry!=reportEntry||rawSHA(node.Body)!=parent.SHA256{return nil,v2input.ErrInvalid}
  var report struct{Proposal v2input.Ref `json:"proposal"`;PNG v2input.Ref `json:"label_png"`;Params struct{Image v2input.Ref `json:"image"`} `json:"params"`}
  if json.Unmarshal(node.Body,&report)!=nil||report.PNG!=entry.Ref||report.Params.Image!=scope.Binding.Source||!validImportRef(report.Proposal){return nil,v2input.ErrInvalid}
  if !preparedPNGChild(node.Children,entry)||!preparedPNGChild(node.Children,v2input.Entry{Ref:report.Proposal,Contract:"DefectCandidates"}){return nil,v2input.ErrInvalid}
  proposalEntry:=v2input.Entry{Ref:report.Proposal,Contract:"DefectCandidates"}
  proposal,err:=s.Documents.ResolveImportNode(ctx,proposalEntry);if err!=nil{return nil,err}
  if proposal.Entry!=proposalEntry||rawSHA(proposal.Body)!=report.Proposal.SHA256{return nil,v2input.ErrInvalid}
  if err:=s.LabelWitnesses.Remember(ctx,proposalEntry,proposal.Body,scope.Binding,proposal.Children);err!=nil{return nil,err}
  verified,err:=review.ParseDefectProposalStructure(ctx,proposal.Body,review.ArtifactRef{ID:report.Proposal.ArtifactID,SHA256:report.Proposal.SHA256});if err!=nil{return nil,err}
  labels,err:=verified.LabelBinding();if err!=nil{return nil,err}
  labelEntry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:labels.Labels.ID,SHA256:labels.Labels.SHA256},Contract:"DefectLabelMap"}
  authorized,err:=s.Labels.ResolveExportDefectLabels(ctx,labelEntry);if err!=nil{return nil,err}
  matched:=false;for _,candidate:=range authorized{b,e:=candidate.LabelBinding();if e!=nil{return nil,e};if b==labels{matched=true}}
  if !matched{return nil,v2input.ErrInvalid}
  object,err:=s.Catalog.LookupImportDocument(ctx,labelEntry);if err!=nil{return nil,err}
  result=append(result,ExportDefectPNGInput{Report:parent,Proposal:verified,Labels:object})
 }
 after,err:=s.Catalog.LookupImportDocument(ctx,entry);if err!=nil||after!=before{return nil,v2input.ErrInvalid}
 return result,ctx.Err()
}
func preparedPNGChild(children []v2input.Entry,required v2input.Entry)bool{
 count:=0;for _,child:=range children{if child.Ref.ArtifactID==required.Ref.ArtifactID{if child!=required{return false};count++}};return count==1
}
var _ ExportDefectPNGSource=SQLPreparedDefectPNGSource{}
