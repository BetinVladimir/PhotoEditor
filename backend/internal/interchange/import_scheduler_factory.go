package interchange
import (
 "context"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/workflow"
)
type NativeImportSchedulerFactory struct{Deployment NativeImportDeployment;Recipes ImportSchedulerRecipeFactory;Limits ImportPlanLimits}
func(f NativeImportSchedulerFactory)BuildSchedulerInterchangeImport(ctx context.Context,db *pgxpool.Pool,objects *objectstore.Store,bucket string)(workflow.InterchangeImportContinuation,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if f.Recipes==nil||db==nil||objects==nil||f.Limits.ArchiveBytes<1||f.Limits.ArchiveBytes>64<<30||f.Limits.ExpandedBytes<1||f.Limits.ExpandedBytes>128<<30||f.Limits.Files<1||f.Limits.Files>30000{return nil,v2input.ErrInvalid}
 validator,ok:=f.Recipes.(ImportRecipeInstallationValidator);if !ok||validator==nil{return nil,v2input.ErrInvalid}
 if err:=validator.ValidateImportRecipeInstallation(ctx);err!=nil{return nil,err}
 deps,resources,err:=f.Deployment.Build(ctx,db,objects,bucket);if err!=nil{return nil,err}
 return NativeImportSchedulerContinuation{Dependencies:deps,Objects:objects,Recipes:f.Recipes,Resources:resources,Limits:f.Limits},nil
}
var _ workflow.SchedulerInterchangeImportFactory=NativeImportSchedulerFactory{}
