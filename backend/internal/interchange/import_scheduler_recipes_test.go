package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/workflow"
)
func TestSQLImportSchedulerRecipesRequireEntryAuthority(t *testing.T){
 resolver,err:=(SQLImportSchedulerRecipeFactory{}).BuildImportSchedulerRecipes(context.Background(),workflow.InterchangeImportCandidate{},nil,ImportResolverDependencies{})
 if err==nil||resolver!=nil{t.Fatal("missing entry authority installed")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err=(SQLImportSchedulerRecipeFactory{}).BuildImportSchedulerRecipes(ctx,workflow.InterchangeImportCandidate{},nil,ImportResolverDependencies{})
 if err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
func TestScopedSQLImportRecipeResolverRefusesMissingScope(t *testing.T){
 var resolver *scopedSQLImportRecipes
 if ref,err:=resolver.ResolvePreparedImportRecipe(context.Background(),uuid.New(),v2input.SessionVersionBinding{});err==nil||ref!=nil{t.Fatal("missing scoped resolver used")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=resolver.ResolvePreparedImportRecipe(ctx,uuid.Nil,v2input.SessionVersionBinding{});err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
