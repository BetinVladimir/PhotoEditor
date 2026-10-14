package interchange
import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type exportRecipeBinding struct{ref v2input.Ref;binding v2input.SessionVersionBinding}
type ExportRecipeDocumentValidator struct{recipes map[uuid.UUID]exportRecipeBinding;Dependencies ExportTypedDocumentValidator}
func NewExportRecipeDocumentValidator(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,dependencies ExportTypedDocumentValidator)(ExportRecipeDocumentValidator,error){
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return ExportRecipeDocumentValidator{},err}
 var plan importPlanDocument;if err:=decodeImportDocument(compiled.planBody,&plan);err!=nil{return ExportRecipeDocumentValidator{},err}
 recipes:=map[uuid.UUID]exportRecipeBinding{}
 for _,entry:=range plan.Entries{
  if entry.Recipe==nil||entry.Binding==nil{return ExportRecipeDocumentValidator{},v2input.ErrInvalid}
  receipt:=exportRecipeBinding{ref:*entry.Recipe,binding:*entry.Binding}
  if old,exists:=recipes[receipt.ref.ArtifactID];exists&&old!=receipt{return ExportRecipeDocumentValidator{},v2input.ErrInvalid}
  recipes[receipt.ref.ArtifactID]=receipt
 }
 return ExportRecipeDocumentValidator{recipes:recipes,Dependencies:dependencies},nil
}
func(v ExportRecipeDocumentValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if entry.Contract!="EditingRecipeV3"{
  if v.Dependencies==nil{return nil,v2input.ErrInvalid}
  return v.Dependencies.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...))
 }
 expected,ok:=v.recipes[entry.Ref.ArtifactID]
 if !ok||entry.Ref!=expected.ref||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 return interchangeRecipeChildren(ctx,raw,expected.binding)
}
// Shared typed child vocabulary for import/export; callers supply independently
// pinned recipe identity and current plan binding before entering this helper.
func interchangeRecipeChildren(ctx context.Context,raw []byte,binding v2input.SessionVersionBinding)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if err:=v2input.ValidateEditingRecipeV3(raw,binding);err!=nil{return nil,err}
 var recipe struct{Source v2input.SessionVersionBinding `json:"source"`;Base v2input.Ref `json:"base_style"`;Corrections *v2input.Ref `json:"session_corrections"`;Learning *uuid.UUID `json:"learning_version_id"`;Overrides []v2input.Ref `json:"overrides"`;Layers *v2input.Ref `json:"layer_stack"`;Protected *v2input.Ref `json:"protected_mask"`}
 if err:=json.Unmarshal(raw,&recipe);err!=nil{return nil,err}
 if recipe.Learning!=nil{return nil,v2input.ErrInvalid}
 children:=[]v2input.Entry{};seen:=map[uuid.UUID]v2input.Entry{}
 add:=func(ref v2input.Ref,contract string)error{
  child:=v2input.Entry{Ref:ref,Contract:contract}
  if old,exists:=seen[ref.ArtifactID];exists{if old!=child{return v2input.ErrInvalid};return nil}
  seen[ref.ArtifactID]=child;children=append(children,child);return nil
 }
 if err:=add(recipe.Source.Source,"WorkingImage");err!=nil{return nil,err}
 if err:=add(recipe.Base,"StyleV2");err!=nil{return nil,err}
 for _,item:=range []struct{ref *v2input.Ref;contract string}{{recipe.Corrections,"CorrectionPlan"},{recipe.Layers,"LayerStack"},{recipe.Protected,"MaskRef"}}{if item.ref!=nil{if err:=add(*item.ref,item.contract);err!=nil{return nil,err}}}
 for _,ref:=range recipe.Overrides{if err:=add(ref,"Override");err!=nil{return nil,err}}
 return children,ctx.Err()
}
