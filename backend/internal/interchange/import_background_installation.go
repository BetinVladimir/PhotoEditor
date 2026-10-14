package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

type ImportBackgroundInstallation struct {
 Catalog ImportDocumentCatalog
 Native ImportBackgroundRasterQualifier
 Committer ImportBackgroundCommitter
 Prepared *PreparedImportArtifactPublisher
}
// Build does no object IO and does not register assets. Every publication
// invocation subsequently requalifies the scheduler binding and staged image.
func(i ImportBackgroundInstallation)Build(ctx context.Context,workflow uuid.UUID,parsed []byte,bindings []ImportCorrectionBinding,sources ImportRecipeSourceQualifier)(IndexedImportBackgroundRemapper,error){
 if err:=ctx.Err();err!=nil{return IndexedImportBackgroundRemapper{},err}
 if workflow==uuid.Nil||i.Catalog==nil||i.Native==nil||(i.Committer==nil)==(i.Prepared==nil){return IndexedImportBackgroundRemapper{},v2input.ErrInvalid}
 authority,err:=NewBoundImportBackgroundAuthority(bindings,sources);if err!=nil{return IndexedImportBackgroundRemapper{},err}
 index,err:=NewImportBackgroundIndex(ctx,parsed);if err!=nil{return IndexedImportBackgroundRemapper{},err}
 committer:=i.Committer
 if i.Prepared!=nil{
  prepared:=*i.Prepared
  if prepared.Scope.Workflow!=workflow||prepared.SourceObjects==nil{return IndexedImportBackgroundRemapper{},v2input.ErrInvalid}
  if _,err:=prepared.scope(prepared.Scope.Entry,"StyleBackgroundAsset");err!=nil{return IndexedImportBackgroundRemapper{},err}
  matched:=false
  for _,binding:=range bindings{if binding.Entry==prepared.Scope.Entry&&binding.New==prepared.Scope.Binding{matched=true}}
  if !matched{return IndexedImportBackgroundRemapper{},v2input.ErrInvalid}
  committer=PreparedImportBackgroundCommitter{Publisher:prepared,Catalog:i.Catalog,Authority:authority}
 }
 publisher:=QualifiedImportBackgroundPublisher{Catalog:i.Catalog,Native:i.Native,Authority:authority,Committer:committer}
 return IndexedImportBackgroundRemapper{Workflow:workflow,Index:index,Publisher:publisher},ctx.Err()
}
