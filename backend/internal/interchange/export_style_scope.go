package interchange

import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// FrozenReader resolves only FrozenRenderRecipe parents and must not recursively
// resolve their children. Policy still checks background/runtime/model authority.
type ExportFrozenStyleScope struct{
 reader ExportClosureNodeResolver
 recipes []exportRecipeBinding
 policy ExportTypedDocumentValidator
 backgrounds ExportStyleBackgroundAuthority
}
func NewExportFrozenStyleScope(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,reader ExportClosureNodeResolver,policy ExportTypedDocumentValidator)(*ExportFrozenStyleScope,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if reader==nil||policy==nil{return nil,v2input.ErrInvalid}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return nil,err}
 var payload exportFreezePayload;if err:=json.Unmarshal(checked.Body(),&payload);err!=nil{return nil,err}
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument([]byte(payload.SnapshotJCS),&snapshot);err!=nil{return nil,err}
 recipes:=make([]exportRecipeBinding,0,len(snapshot.Items))
 for _,item:=range snapshot.Items{recipes=append(recipes,exportRecipeBinding{ref:item.Recipe,binding:item.Binding})}
 return &ExportFrozenStyleScope{reader:reader,recipes:recipes,policy:policy},ctx.Err()
}
func(s *ExportFrozenStyleScope)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s==nil||s.reader==nil||s.policy==nil{return nil,v2input.ErrInvalid}
 if entry.Contract!="StyleV2"{return s.policy.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...))}
 if !validImportRef(entry.Ref)||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 matched:=false
 for _,expected:=range s.recipes{
  parent:=v2input.Entry{Ref:expected.ref,Contract:"FrozenRenderRecipe"}
  node,err:=s.reader.ResolveExportNode(ctx,parent);if err!=nil{return nil,err}
  if node.Entry!=parent||rawSHA(node.Body)!=parent.Ref.SHA256{return nil,v2input.ErrInvalid}
  recipe,err:=v2draft.DecodeFrozenRenderRecipe(node.Body);if err!=nil{return nil,err}
  if recipe.Scalars.Source!=expected.binding{return nil,v2input.ErrInvalid}
  if recipe.Scalars.BaseStyle==entry.Ref{
   if err:=qualifyExportStyleBackground(ctx,raw,entry.Ref,recipe,s.backgrounds);err!=nil{return nil,err}
   found:=false;for _,child:=range node.Children{if child.Ref.ArtifactID==entry.Ref.ArtifactID{if child!=entry{return nil,v2input.ErrInvalid};found=true}}
   if !found{return nil,v2input.ErrInvalid};matched=true
  }
 }
 if !matched{return nil,v2input.ErrInvalid}
 children,err:=s.policy.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...));if err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err};return append([]v2input.Entry(nil),children...),nil
}
var _ ExportTypedDocumentValidator=(*ExportFrozenStyleScope)(nil)
