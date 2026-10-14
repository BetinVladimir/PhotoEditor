package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)
// Deployment-only file references. Neither path nor SHA is accepted from jobs.
type ImportRestorationDeploymentFiles struct{DenoiseFile,DenoiseSHA,UpscaleFile,UpscaleSHA string}
func(f ImportRestorationDeploymentFiles)Install(ctx context.Context,base SQLImportEntryRecipeAuthorityFactory)(SQLImportEntryRecipeAuthorityFactory,error){
 var zero SQLImportEntryRecipeAuthorityFactory
 if err:=ctx.Err();err!=nil{return zero,err}
 if (f.DenoiseFile=="")!=(f.DenoiseSHA=="")||(f.UpscaleFile=="")!=(f.UpscaleSHA==""){return zero,v2input.ErrInvalid}
 // Do not override an existing separately configured policy or operation key.
 // Load everything before changing the returned factory's configuration.
 var denoise *ImportDenoiseQualificationInstallation
 var upscale ImportUpscaleQualifications
 if f.DenoiseFile!=""{
  if base.Codecs.DenoiseQualification!=nil||base.Codecs.DenoiseModelLock!=""{return zero,v2input.ErrInvalid}
  if base.Codecs.Operations!=nil{if _,exists:=base.Codecs.Operations.operations[ImportOperationVersion{"image.denoise","1"}];exists{return zero,v2input.ErrInvalid}}
  config,err:=LoadImportDenoiseQualification(ctx,f.DenoiseFile,f.DenoiseSHA);if err!=nil{return zero,err};denoise=&config
 }
 if f.UpscaleFile!=""{
  if base.Codecs.UpscaleQualifications!=nil||base.Codecs.UpscaleModelLocks!=nil{return zero,v2input.ErrInvalid}
  if base.Codecs.Operations!=nil{if _,exists:=base.Codecs.Operations.operations[ImportOperationVersion{"image.upscale","1"}];exists{return zero,v2input.ErrInvalid}}
  config,err:=LoadImportUpscaleQualifications(ctx,f.UpscaleFile,f.UpscaleSHA);if err!=nil{return zero,err};upscale=config
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 result:=base
 if denoise!=nil{result.Codecs.DenoiseQualification=denoise}
 if upscale!=nil{result.Codecs.UpscaleQualifications=upscale}
 return result,nil
}
