package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Installed authority must retain current learning eligibility, training consent,
// analysis/source and registered frozen-parent identity, not just model bytes.
type ExportLearningOccurrenceAuthority interface {
 QualifyExportLearningOccurrence(context.Context,v2draft.LayerRef,[]byte)error
}
type ExportFrozenLearningSemantics struct {
 reader ExportClosureNodeResolver
 recipes []exportRecipeBinding
 authority ExportLearningOccurrenceAuthority
 fallback ExportDependencySemantics
}
func NewExportFrozenLearningSemantics(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,reader ExportClosureNodeResolver,authority ExportLearningOccurrenceAuthority,fallback ExportDependencySemantics)(*ExportFrozenLearningSemantics,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if reader==nil||authority==nil||fallback==nil{return nil,v2input.ErrInvalid}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return nil,err}
 var payload exportFreezePayload;if err:=json.Unmarshal(checked.Body(),&payload);err!=nil{return nil,err}
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument([]byte(payload.SnapshotJCS),&snapshot);err!=nil{return nil,err}
 result:=&ExportFrozenLearningSemantics{reader:reader,authority:authority,fallback:fallback}
 for _,item:=range snapshot.Items{result.recipes=append(result.recipes,exportRecipeBinding{ref:item.Recipe,binding:item.Binding})}
 return result,ctx.Err()
}
func(s *ExportFrozenLearningSemantics)QualifyExportDependency(ctx context.Context,entry v2input.Entry,raw []byte,hints []v2draft.LayerRef)error{
 if err:=ctx.Err();err!=nil{return err}
 if s==nil||s.reader==nil||s.authority==nil||s.fallback==nil{return v2input.ErrInvalid}
 if entry.Contract=="CandidateProfile"||entry.Contract=="DescriptorShard"{
  body:=append([]byte(nil),raw...)
  if !validImportRef(entry.Ref)||len(body)<2||len(body)>8<<20||rawSHA(body)!=entry.Ref.SHA256{return v2input.ErrInvalid}
  normalized,err:=canonical.Bytes(body);if err!=nil||!bytes.Equal(normalized,body){return v2input.ErrInvalid}
  matched:=false
  for _,expected:=range s.recipes{
   parent:=v2input.Entry{Ref:expected.ref,Contract:"FrozenRenderRecipe"}
   node,err:=s.reader.ResolveExportNode(ctx,parent);if err!=nil{return err}
   if node.Entry!=parent{return v2input.ErrInvalid}
   frozen,err:=v2draft.DecodeFrozenRenderRecipe(node.Body);if err!=nil||frozen.Scalars.Source!=expected.binding{return v2input.ErrInvalid}
   dependencies,err:=v2draft.FrozenRenderRecipeDependencies(node.Body,parent.Ref);if err!=nil{return err}
   for _,dependency:=range dependencies{
    if dependency.Ref.ArtifactID!=entry.Ref.ArtifactID||dependency.Contract!=entry.Contract{continue}
    if dependency.Ref!=entry.Ref||dependency.ExpectedLearning==nil||dependency.ExpectedBinding==nil||*dependency.ExpectedBinding!=expected.binding{return v2input.ErrInvalid}
    edge:=false
    for _,child:=range node.Children{if child.Ref.ArtifactID==entry.Ref.ArtifactID{if child!=entry{return v2input.ErrInvalid};edge=true}}
    if !edge{return v2input.ErrInvalid}
    if _,err:=v2draft.DocumentOccurrenceChildren(body,dependency);err!=nil{return err}
    if err:=s.authority.QualifyExportLearningOccurrence(ctx,dependency,append([]byte(nil),body...));err!=nil{return err}
    matched=true
   }
  }
  if !matched{return v2input.ErrInvalid}
 }
 if err:=s.fallback.QualifyExportDependency(ctx,entry,append([]byte(nil),raw...),hints);err!=nil{return err}
 return ctx.Err()
}
func(s SQLExportDocumentCatalog)QualifyExportLearningOccurrence(ctx context.Context,node v2draft.LayerRef,raw []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if (node.Contract!="CandidateProfile"&&node.Contract!="DescriptorShard")||node.ExpectedBinding==nil||node.ExpectedLearning==nil||!validImportRef(node.ParentRef)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=node.Ref.SHA256{return v2input.ErrInvalid}
 entry:=v2input.Entry{Ref:node.Ref,Contract:node.Contract}
 current,err:=s.LookupExportDocument(ctx,entry);if err!=nil{return err}
 catalog:=v2draft.SQLArtifactCatalog{DB:s.DB,Bucket:s.Bucket,ProjectID:s.Request.ProjectID,AssetVersionID:node.ExpectedBinding.AssetVersionID}
 qualified,err:=catalog.Lookup(ctx,s.Who,s.Tenant,node);if err!=nil{return err}
 if qualified!=current||qualified.Bytes!=int64(len(raw)){return v2input.ErrInvalid}
 return ctx.Err()
}
var _ ExportLearningOccurrenceAuthority=SQLExportDocumentCatalog{}
var _ ExportDependencySemantics=(*ExportFrozenLearningSemantics)(nil)
