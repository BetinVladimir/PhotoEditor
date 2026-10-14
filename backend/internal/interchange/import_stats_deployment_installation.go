package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)
// Optional process-owned configuration. Cannot replace an existing independent
// policy or operation key, and returns no partial factory on loader failure.
type ImportStatsDeploymentFile struct{Path,SHA string}
func(f ImportStatsDeploymentFile)Install(ctx context.Context,base SQLImportEntryRecipeAuthorityFactory)(SQLImportEntryRecipeAuthorityFactory,error){
 var zero SQLImportEntryRecipeAuthorityFactory
 if err:=ctx.Err();err!=nil{return zero,err}
 if (f.Path=="")!=(f.SHA==""){return zero,v2input.ErrInvalid}
 if f.Path==""{return base,nil}
 if base.Codecs.StatsQualification!=nil{return zero,v2input.ErrInvalid}
 if base.Codecs.Operations!=nil{if _,exists:=base.Codecs.Operations.operations[ImportOperationVersion{"color.stats","1"}];exists{return zero,v2input.ErrInvalid}}
 config,err:=LoadImportStatsQualification(ctx,f.Path,f.SHA);if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 base.Codecs.StatsQualification=&config
 return base,nil
}
