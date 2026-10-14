package interchange

import (
 "context"
 "errors"
 "unicode/utf8"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type ImportPlanLimits struct{ArchiveBytes,ExpandedBytes int64;Files int}
type ImportMasterReader interface{ReadImportMaster(context.Context,v2input.Ref,int64)([]byte,error)}
// The resolver remaps and qualifies original recipe dependencies against the
// supplied canonical binding. Unsupported recipes return ImportRecipeUnsupported;
// transport/authorization/qualification failures must remain errors. A nil
// receipt without a typed diagnostic is invalid, never an invented recipe.
type ImportPreparedRecipeResolver interface{ResolvePreparedImportRecipe(context.Context,uuid.UUID,v2input.SessionVersionBinding)(*v2input.Ref,error)}
// ImportRecipeUnsupported is an explicit compiler outcome, not an IO failure.
// Reasons are bounded report text, validated before they enter frozen evidence.
type ImportRecipeUnsupported struct{Reason string}
func(e *ImportRecipeUnsupported)Error()string{if e==nil{return "INTERCHANGE_RECIPE_UNSUPPORTED"};return "INTERCHANGE_RECIPE_UNSUPPORTED: "+e.Reason}
type CompiledImportPlan struct{body []byte;sources ImportSourceInventory;diagnostics []importRecipeDiagnostic}
func(p CompiledImportPlan)DiagnosticsBody()([]byte,error){
 diagnostics:=append([]importRecipeDiagnostic{},p.diagnostics...)
 return canonical.Marshal(diagnostics)
}
func(p CompiledImportPlan)Body()[]byte{return append([]byte(nil),p.body...)}
func(p CompiledImportPlan)SourcesBody()[]byte{return p.sources.Body()}
func(p CompiledImportPlan)Documents(identity ImportDocumentIdentity)(ImportPreparedDocuments,error){
 if identity.Workflow!=p.sources.document.Workflow{return ImportPreparedDocuments{},v2input.ErrInvalid}
 return FreezeImportDocuments(identity,p.sources.document.Parsed,p.body)
}

// CompileImportPlan preserves every manifest entry in original order. Terminal
// ingest entries do not trigger master/recipe IO. Source evidence is retained
// beside the plan because nullable bindings do not encode terminal reasons.
func CompileImportPlan(ctx context.Context,sources ImportSourceInventory,parsedRaw []byte,snapshot string,limits ImportPlanLimits,masters ImportMasterReader,recipes ImportPreparedRecipeResolver)(CompiledImportPlan,error){
 if err:=ctx.Err();err!=nil{return CompiledImportPlan{},err}
 parsedRaw=append([]byte(nil),parsedRaw...)
 frozen,err:=freezeImportSources(ctx,sources.document.Workflow,sources.document.Project,sources.document.Parsed,parsedRaw,sources.body);if err!=nil{return CompiledImportPlan{},err}
 if !validImportRef(v2input.Ref{ArtifactID:frozen.document.Project,SHA256:snapshot})||limits.ArchiveBytes<1||limits.ArchiveBytes>64<<30||limits.ExpandedBytes<1||limits.ExpandedBytes>128<<30||limits.Files<1||limits.Files>30000{return CompiledImportPlan{},v2input.ErrInvalid}
 var parsed importParsedDocument;if err:=decodeImportDocument(parsedRaw,&parsed);err!=nil{return CompiledImportPlan{},err}
 diagnostics:=[]importRecipeDiagnostic{}
 files:=map[string]v2input.Ref{};for _,file:=range parsed.Files{files[file.Path]=file.Content}
 format:=frozen.document.Format
 if format==""{format="photoeditor_bundle"}
 if !supportedParseFormat(format){return CompiledImportPlan{},v2input.ErrInvalid}
 plan:=importPlanDocument{Schema:1,Project:frozen.document.Project,Direction:"import",Format:format,Snapshot:snapshot,Archive:limits.ArchiveBytes,Expanded:limits.ExpandedBytes,Files:limits.Files,Entries:make([]importPlanEntry,0,len(parsed.Manifest.Entries))}
 for ordinal,original:=range parsed.Manifest.Entries{
  if err:=ctx.Err();err!=nil{return CompiledImportPlan{},err}
  source:=frozen.document.Entries[ordinal]
  entry:=importPlanEntry{ID:original.ID,External:original.External,Path:original.ImagePath,Content:files[original.ImagePath],Rating:original.Rating,Pick:original.Pick,Crop:original.Crop}
  if source.State=="adopted"{
   if masters==nil{return CompiledImportPlan{},v2input.ErrInvalid}
   raw,err:=masters.ReadImportMaster(ctx,*source.Master,1<<20);if err!=nil{return CompiledImportPlan{},err}
   if err:=ctx.Err();err!=nil{return CompiledImportPlan{},err}
   if len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=source.Master.SHA256{return CompiledImportPlan{},v2input.ErrInvalid}
   var typed review.WorkingImage;if err:=decodeImportDocument(raw,&typed);err!=nil{return CompiledImportPlan{},err}
   image,err:=review.ParseWorkingImage(raw);if err!=nil{return CompiledImportPlan{},err}
   if image.Canvas.SourceSHA256!=original.ImageSHA||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"{return CompiledImportPlan{},v2input.ErrInvalid}
   canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return CompiledImportPlan{},err}
   binding:=v2input.SessionVersionBinding{AssetVersionID:*source.Version,Source:*source.Master,CanvasHash:canvas};entry.Binding=&binding
   if original.RecipePath!=nil{
    if recipes==nil{return CompiledImportPlan{},v2input.ErrInvalid}
    receipt,resolveErr:=recipes.ResolvePreparedImportRecipe(ctx,original.ID,binding)
    if err:=ctx.Err();err!=nil{return CompiledImportPlan{},err}
    if resolveErr!=nil{
     var unsupported *ImportRecipeUnsupported
     if !errors.As(resolveErr,&unsupported){return CompiledImportPlan{},resolveErr}
     if unsupported==nil||receipt!=nil||!utf8.ValidString(unsupported.Reason)||utf8.RuneCountInString(unsupported.Reason)<1||utf8.RuneCountInString(unsupported.Reason)>200{return CompiledImportPlan{},v2input.ErrInvalid}
     diagnostics=append(diagnostics,importRecipeDiagnostic{ID:original.ID,Reason:unsupported.Reason})
    }else{
     if receipt==nil||!validImportRef(*receipt){return CompiledImportPlan{},v2input.ErrInvalid}
     owned:=*receipt;entry.Recipe=&owned
    }
   }
  }
  plan.Entries=append(plan.Entries,entry)
 }
 raw,err:=canonical.Marshal(plan);if err!=nil||len(raw)>8<<20{return CompiledImportPlan{},v2input.ErrInvalid}
 if _,err:=NewImportScopedDocumentValidator(plan.Project,snapshot,raw,parsedRaw);err!=nil{return CompiledImportPlan{},err}
 return CompiledImportPlan{body:raw,sources:frozen,diagnostics:diagnostics},ctx.Err()
}
