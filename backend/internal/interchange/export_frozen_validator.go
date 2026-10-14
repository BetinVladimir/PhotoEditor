package interchange
import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type ExportFrozenDocumentValidator struct{recipes map[uuid.UUID]exportRecipeBinding;icc,model string;Dependencies ExportTypedDocumentValidator}
func NewExportFrozenDocumentValidator(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,icc,model string,dependencies ExportTypedDocumentValidator)(ExportFrozenDocumentValidator,error){
 for _,digest:=range []string{icc,model}{if !validImportRef(v2input.Ref{ArtifactID:intent.Identity.Task,SHA256:digest}){return ExportFrozenDocumentValidator{},v2input.ErrInvalid}}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return ExportFrozenDocumentValidator{},err}
 var payload exportFreezePayload;if err:=json.Unmarshal(checked.Body(),&payload);err!=nil{return ExportFrozenDocumentValidator{},err}
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument([]byte(payload.SnapshotJCS),&snapshot);err!=nil{return ExportFrozenDocumentValidator{},err}
 recipes:=map[uuid.UUID]exportRecipeBinding{}
 for _,item:=range snapshot.Items{
  receipt:=exportRecipeBinding{ref:item.Recipe,binding:item.Binding}
  if old,exists:=recipes[receipt.ref.ArtifactID];exists&&old!=receipt{return ExportFrozenDocumentValidator{},v2input.ErrInvalid}
  recipes[receipt.ref.ArtifactID]=receipt
 }
 return ExportFrozenDocumentValidator{recipes:recipes,icc:icc,model:model,Dependencies:dependencies},nil
}
func(v ExportFrozenDocumentValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if entry.Contract!="FrozenRenderRecipe"{
  if v.Dependencies==nil{return nil,v2input.ErrInvalid};return v.Dependencies.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...))
 }
 expected,ok:=v.recipes[entry.Ref.ArtifactID]
 if !ok||entry.Ref!=expected.ref||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 frozen,err:=v2draft.DecodeFrozenRenderRecipe(raw);if err!=nil{return nil,err}
 if frozen.Scalars.Source!=expected.binding||frozen.Scalars.ICCLockSHA!=v.icc||frozen.Scalars.ModelLockSHA!=v.model{return nil,v2input.ErrInvalid}
 refs,err:=v2draft.DocumentChildren(raw,"FrozenRenderRecipe",entry.Ref);if err!=nil{return nil,err}
 children:=[]v2input.Entry{};seen:=map[uuid.UUID]v2input.Entry{}
 for _,ref:=range refs{
  child:=v2input.Entry{Ref:ref.Ref,Contract:ref.Contract}
  if old,exists:=seen[child.Ref.ArtifactID];exists{if old!=child{return nil,v2input.ErrInvalid};continue}
  seen[child.Ref.ArtifactID]=child;children=append(children,child)
 }
 return children,ctx.Err()
}
