package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
)
func TestSQLImportExecutionInstallationRequiresExecutableEvidence(t *testing.T){
 documents,_:=importMaterializationFixture(t)
 p,err:=NewSQLImportExecutionPreparation(context.Background(),uuid.New(),documents,[]byte(`{}`),ImportResolverDependencies{},nil)
 if err==nil||p.Resolver!=nil{t.Fatal("incomplete execution installed")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err=NewSQLImportExecutionPreparation(ctx,uuid.New(),documents,nil,ImportResolverDependencies{},nil)
 if err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
