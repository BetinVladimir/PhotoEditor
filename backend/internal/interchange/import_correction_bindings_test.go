package interchange
import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/workflow"
)
func TestCorrectionBindingDerivationPreservesTerminalEntries(t *testing.T){
 w,p,ref,parsed,document:=importSourcesFixture(t)
 raw,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)}
 sources,err:=freezeImportSources(context.Background(),w,p,ref,parsed,raw);if err!=nil{t.Fatal(err)}
 archive:=&sourceArchiveReader{};masters:=&recipeSourceMasterReader{}
 bindings,err:=deriveImportCorrectionBindings(context.Background(),sources,parsed,archive,masters)
 if err!=nil||len(bindings)!=0||archive.calls!=0||masters.calls!=0{t.Fatalf("terminal entry became dependency: %v",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=deriveImportCorrectionBindings(ctx,sources,parsed,archive,masters);!errors.Is(err,context.Canceled){t.Fatal(err)}
 if _,err:=deriveImportCorrectionBindings(context.Background(),sources,parsed,nil,masters);err==nil{t.Fatal("missing archive accepted")}
 if _,err:=deriveImportCorrectionBindings(context.Background(),sources,[]byte(`{}`),archive,masters);err==nil{t.Fatal("foreign parsed accepted")}
}
func TestWorkflowRecipeAuthorityBindingRejectsIncompleteInstallation(t *testing.T){
 f:=SQLImportEntryRecipeAuthorityFactory{}
 if authority,err:=f.BindImportWorkflowRecipeAuthority(context.Background(),workflow.InterchangeImportCandidate{},nil,ImportResolverDependencies{});err==nil||authority!=nil{t.Fatal("incomplete binding returned authority")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if authority,err:=f.BindImportWorkflowRecipeAuthority(ctx,workflow.InterchangeImportCandidate{},nil,ImportResolverDependencies{});!errors.Is(err,context.Canceled)||authority!=nil{t.Fatal("cancel binding",err)}
}
