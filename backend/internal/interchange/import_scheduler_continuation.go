package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/workflow"
)
// Must install complete per-workflow source/remap/model/publisher authority.
// Missing recipe support cannot be replaced by silently dropping recipes.
type ImportSchedulerRecipeFactory interface{
 BuildImportSchedulerRecipes(context.Context,workflow.InterchangeImportCandidate,[]byte,ImportResolverDependencies)(ImportPreparedRecipeResolver,error)
}
type NativeImportSchedulerContinuation struct{Dependencies ImportResolverDependencies;Objects ExportExecutionObjects;Recipes ImportSchedulerRecipeFactory;Resources *MeasuredImportResourceQualifier;Limits ImportPlanLimits}
func(s NativeImportSchedulerContinuation)ContinueInterchangeImport(ctx context.Context,c workflow.InterchangeImportCandidate,parsedRaw []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.Recipes==nil||s.Resources==nil||s.Objects==nil||s.Dependencies.DB==nil||c.Bucket!=s.Dependencies.Bucket||rawSHA(parsedRaw)!=c.SHA{return v2input.ErrInvalid}
 parsed:=append([]byte(nil),parsedRaw...)
 deps:=s.Dependencies;deps.Objects=s.Objects
 recipes,err:=s.Recipes.BuildImportSchedulerRecipes(ctx,c,append([]byte(nil),parsed...),deps);if err!=nil{return err}
 if recipes==nil{return v2input.ErrInvalid}
 continuation,err:=NewSQLImportContinuation(ctx,c.Tenant,ImportDocumentIdentity{Run:c.Run,Workflow:c.Workflow},c.Project,v2input.Ref{ArtifactID:c.Artifact,SHA256:c.SHA},parsed,c.Snapshot,s.Limits,deps,s.Objects,recipes,s.Resources);if err!=nil{return err}
 return continuation.ContinueImport(ctx)
}
var _ workflow.InterchangeImportContinuation=NativeImportSchedulerContinuation{}
