package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)
// Process-owned qualification files are loaded as one installation. This does
// not enable operations or invent the independent origin/consent authority.
type ImportRecipeDeploymentFiles struct{
 Restoration ImportRestorationDeploymentFiles
 Stats ImportStatsDeploymentFile
 Person ImportPersonMaskDeploymentFile
}
func(f ImportRecipeDeploymentFiles)Install(ctx context.Context,base SQLImportEntryRecipeAuthorityFactory,personAcceptance ImportMaskQualifier)(SQLImportEntryRecipeAuthorityFactory,error){
 var zero SQLImportEntryRecipeAuthorityFactory
 if err:=ctx.Err();err!=nil{return zero,err}
 // Validate the entire declaration before opening any operator file.
 for _,pair:=range [][2]string{{f.Restoration.DenoiseFile,f.Restoration.DenoiseSHA},{f.Restoration.UpscaleFile,f.Restoration.UpscaleSHA},{f.Stats.Path,f.Stats.SHA},{f.Person.Path,f.Person.SHA}}{
  if (pair[0]=="")!=(pair[1]==""){return zero,v2input.ErrInvalid}
 }
 if f.Person.Path!=""&&personAcceptance==nil{return zero,v2input.ErrInvalid}
 installed,err:=f.Restoration.Install(ctx,base);if err!=nil{return zero,err}
 installed,err=f.Stats.Install(ctx,installed);if err!=nil{return zero,err}
 installed,err=f.Person.Install(ctx,installed,personAcceptance);if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return installed,nil
}
