package interchange
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
// The frozen import plan determines exact local recipe identities/bindings.
// Other dependencies retain their independently installed semantic validator.
type ImportRecipeDocumentValidator struct{recipes map[uuid.UUID]exportRecipeBinding;Dependencies ImportTypedDocumentValidator}
func NewImportRecipeDocumentValidator(ctx context.Context,workflow,project uuid.UUID,snapshot string,planRaw []byte,dependencies ImportTypedDocumentValidator)(ImportRecipeDocumentValidator,error){
 var zero ImportRecipeDocumentValidator
 if err:=ctx.Err();err!=nil{return zero,err}
 if workflow==uuid.Nil||dependencies==nil{return zero,v2input.ErrInvalid}
 raw:=append([]byte(nil),planRaw...)
 roots:=ImportPlanDocumentValidator{Project:project,SnapshotDigest:snapshot}
 if _,err:=roots.ImportDocumentChildren(ctx,v2input.Entry{Contract:"InterchangePlan",Ref:v2input.Ref{ArtifactID:uuid.NewSHA1(workflow,[]byte("interchange:plan:validator")),SHA256:rawSHA(raw)}},raw);err!=nil{return zero,err}
 var plan importPlanDocument;if err:=decodeImportDocument(raw,&plan);err!=nil{return zero,err}
 recipes:=map[uuid.UUID]exportRecipeBinding{}
 for _,item:=range plan.Entries{
  if item.Recipe==nil{continue}
  if item.Binding==nil||item.Recipe.ArtifactID!=uuid.NewSHA1(workflow,[]byte("interchange:recipe:"+item.ID.String())){return zero,v2input.ErrInvalid}
  pinned:=exportRecipeBinding{ref:*item.Recipe,binding:*item.Binding}
  if old,exists:=recipes[pinned.ref.ArtifactID];exists&&old!=pinned{return zero,v2input.ErrInvalid};recipes[pinned.ref.ArtifactID]=pinned
 }
 return ImportRecipeDocumentValidator{recipes:recipes,Dependencies:dependencies},ctx.Err()
}
func(v ImportRecipeDocumentValidator)ImportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if entry.Contract!="EditingRecipeV3"{
  if v.Dependencies==nil{return nil,v2input.ErrInvalid}
  return v.Dependencies.ImportDocumentChildren(ctx,entry,append([]byte(nil),raw...))
 }
 expected,ok:=v.recipes[entry.Ref.ArtifactID]
 if !ok||expected.ref!=entry.Ref||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 return interchangeRecipeChildren(ctx,append([]byte(nil),raw...),expected.binding)
}
