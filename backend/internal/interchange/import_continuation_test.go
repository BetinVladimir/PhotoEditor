package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestSQLImportContinuationRefusesIncompleteInstallation(t *testing.T){
 workflow,project,parsedRef,parsed,_:=importSourcesFixture(t)
 continuation,err:=NewSQLImportContinuation(context.Background(),uuid.New(),ImportDocumentIdentity{Run:uuid.New(),Workflow:workflow},project,parsedRef,parsed,rawSHA([]byte("snapshot")),ImportPlanLimits{ArchiveBytes:1024,ExpandedBytes:2048,Files:10},ImportResolverDependencies{},nil,nil,nil)
 if err==nil||continuation!=nil{t.Fatal("incomplete continuation installed")}
}
func TestSQLImportContinuationCancellationBeforeInstallation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 continuation,err:=NewSQLImportContinuation(ctx,uuid.Nil,ImportDocumentIdentity{},uuid.Nil,v2input.Ref{},nil,"",ImportPlanLimits{},ImportResolverDependencies{},nil,nil,nil)
 if err!=context.Canceled||continuation!=nil{t.Fatal("cancellation lost",err)}
}
func TestSQLImportContinuationNilAndCancellationDoNotExecute(t *testing.T){
 var continuation *SQLImportContinuation
 if err:=continuation.ContinueImport(context.Background());err==nil{t.Fatal("nil continuation executed")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=continuation.ContinueImport(ctx);err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
