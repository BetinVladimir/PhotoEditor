package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestSQLImportCompilerInstallationRequiresRecipeAuthority(t *testing.T){
 workflow,project,parsedRef,parsed,_:=importSourcesFixture(t)
 compiler,err:=NewSQLImportExecutionCompiler(context.Background(),uuid.New(),workflow,project,parsedRef,parsed,ImportResolverDependencies{},nil)
 if err==nil||compiler.Metadata!=nil||compiler.Plan.Sources!=nil{t.Fatal("missing recipe authority installed")}
}
func TestSQLImportCompilerInstallationRejectsSubstitutedParsed(t *testing.T){
 workflow,project,parsedRef,parsed,_:=importSourcesFixture(t)
 recipe:=importRecipeResolverFunc(func(context.Context,uuid.UUID,v2input.SessionVersionBinding)(*v2input.Ref,error){t.Fatal("recipe authority reached during invalid installation");return nil,nil})
 parsedRef.SHA256=rawSHA([]byte("foreign"))
 compiler,err:=NewSQLImportExecutionCompiler(context.Background(),uuid.New(),workflow,project,parsedRef,parsed,ImportResolverDependencies{},recipe)
 if err==nil||compiler.Metadata!=nil||compiler.Plan.Sources!=nil{t.Fatal("substituted parsed installed")}
}
func TestSQLImportCompilerInstallationCancellationBeforeConstruction(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 compiler,err:=NewSQLImportExecutionCompiler(ctx,uuid.Nil,uuid.Nil,uuid.Nil,v2input.Ref{},nil,ImportResolverDependencies{},nil)
 if err!=context.Canceled||compiler.Metadata!=nil||compiler.Plan.Sources!=nil{t.Fatal("cancellation lost",err)}
}
