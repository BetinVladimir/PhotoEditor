package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)
// File supplies qualification pins only. Acceptance/consent is a separately
// installed scoped service and cannot be declared by process JSON or archive.
type ImportPersonMaskDeploymentFile struct{Path,SHA string}
func(f ImportPersonMaskDeploymentFile)Install(ctx context.Context,base SQLImportEntryRecipeAuthorityFactory,consent ImportMaskQualifier)(SQLImportEntryRecipeAuthorityFactory,error){
 var zero SQLImportEntryRecipeAuthorityFactory
 if err:=ctx.Err();err!=nil{return zero,err}
 if (f.Path=="")!=(f.SHA==""){return zero,v2input.ErrInvalid}
 if f.Path==""{return base,nil}
 if consent==nil||base.PersonMaskQualification!=nil||base.MaskSemantics!=nil{return zero,v2input.ErrInvalid}
 if base.MaskInstallation!=nil{if _,exists:=base.MaskInstallation.Models["person"];exists{return zero,v2input.ErrInvalid}}
 config,err:=LoadImportPersonMaskQualification(ctx,f.Path,f.SHA,consent);if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 base.PersonMaskQualification=&config
 return base,nil
}
