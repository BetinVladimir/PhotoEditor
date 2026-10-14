package interchange
import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestImportRecipeCodecGraphRequiresCompleteAuthority(t *testing.T){
 if _,err:=NewImportRecipeCodecGraph(context.Background(),ImportRecipeCodecDependencies{});!errors.Is(err,v2input.ErrInvalid){t.Fatalf("missing authority: %v",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=NewImportRecipeCodecGraph(ctx,ImportRecipeCodecDependencies{});!errors.Is(err,context.Canceled){t.Fatalf("cancel: %v",err)}
}
func TestSQLImportEntryRecipeAuthorityRefusesMissingInfrastructure(t *testing.T){
 codec,qualifier,err:=NewSQLImportEntryRecipeAuthority(context.Background(),PreparedImportResolverDependencies{},ImportRecipeCodecDependencies{})
 if !errors.Is(err,v2input.ErrInvalid)||codec!=nil||qualifier!=nil{t.Fatalf("partial authority returned: %v",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 codec,qualifier,err=NewSQLImportEntryRecipeAuthority(ctx,PreparedImportResolverDependencies{},ImportRecipeCodecDependencies{})
 if !errors.Is(err,context.Canceled)||codec!=nil||qualifier!=nil{t.Fatalf("cancel returned authority: %v",err)}
}
func TestImportRecipeCodecGraphRefusesEmptyOperationRouter(t *testing.T){
 // Non-nil must not let an empty registry masquerade as installed semantics.
 _,err:=NewImportRecipeCodecGraph(context.Background(),ImportRecipeCodecDependencies{Operations:&ImportOperationRouter{}})
 if !errors.Is(err,v2input.ErrInvalid){t.Fatal("empty operation admission accepted",err)}
}
