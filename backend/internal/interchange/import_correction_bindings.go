package interchange
import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Archived recipes and their CorrectionPlans supply old/new edit bindings.
// Rejected entries stay terminal plan outcomes and cannot become dependencies.
func deriveImportCorrectionBindings(ctx context.Context,sources ImportSourceInventory,parsedRaw []byte,archive ImportArchiveDocumentReader,masters ImportMasterReader)([]ImportCorrectionBinding,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if archive==nil||masters==nil||len(sources.body)<2{return nil,v2input.ErrInvalid}
 frozen,err:=freezeImportSources(ctx,sources.document.Workflow,sources.document.Project,sources.document.Parsed,parsedRaw,sources.Body());if err!=nil{return nil,err}
 var parsed importParsedDocument;if err:=decodeImportDocument(parsedRaw,&parsed);err!=nil{return nil,err}
 files:=map[string]importParsedFile{};for _,file:=range parsed.Files{files[file.Path]=file}
 qualifier,err:=NewParsedImportRecipeSourceQualifier(parsedRaw,archive,masters);if err!=nil{return nil,err}
 result:=[]ImportCorrectionBinding{}
 plans:=[]importArchivedCorrectionRoot{}
 for ordinal,item:=range frozen.document.Entries{
  if err:=ctx.Err();err!=nil{return nil,err}
  entry:=parsed.Manifest.Entries[ordinal]
  if item.State!="adopted"||entry.RecipePath==nil{continue}
  file:=files[*entry.RecipePath]
  raw,err:=archive.ReadImportArchiveDocument(ctx,file.Content,1<<20);if err!=nil{return nil,err};raw=append([]byte(nil),raw...)
  if len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=file.Content.SHA256{return nil,v2input.ErrInvalid}
  var recipe struct{Source v2input.SessionVersionBinding `json:"source"`;Corrections *v2input.Ref `json:"session_corrections"`}
  if err:=json.Unmarshal(raw,&recipe);err!=nil{return nil,err}
  if err:=v2input.ValidateEditingRecipeV3(raw,recipe.Source);err!=nil{return nil,err}
  master,err:=masters.ReadImportMaster(ctx,*item.Master,1<<20);if err!=nil{return nil,err};master=append([]byte(nil),master...)
  if len(master)<2||len(master)>1<<20||rawSHA(master)!=item.Master.SHA256{return nil,v2input.ErrInvalid}
  image,err:=review.ParseWorkingImage(master);if err!=nil{return nil,err}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return nil,err}
  target:=v2input.SessionVersionBinding{AssetVersionID:*item.Version,Source:*item.Master,CanvasHash:canvas}
  if err:=qualifier.QualifyImportRecipeSource(ctx,item.ID,recipe.Source,target);err!=nil{return nil,err}
  // The archive reader also repeats its own current object catalogue lookup.
  current,err:=archive.ReadImportArchiveDocument(ctx,file.Content,1<<20);if err!=nil{return nil,err}
  if string(current)!=string(raw){return nil,v2input.ErrInvalid}
  result=append(result,ImportCorrectionBinding{Entry:item.ID,Old:recipe.Source,New:target})
  if recipe.Corrections!=nil{plans=append(plans,importArchivedCorrectionRoot{Ref:*recipe.Corrections,Binding:recipe.Source})}
 }
 return extendImportCorrectionBindings(ctx,frozen,parsed,qualifier,result,plans)
}
