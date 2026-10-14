package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
)
func TestSQLImportActivationRejectsMissingPrivateQualification(t *testing.T){
 if err:=(SQLImportActivator{Tenant:uuid.New()}).ActivateImport(context.Background(),QualifiedImportActivation{});err==nil{t.Fatal("missing qualification activated")}
}
func TestSQLImportActivationCancellationBeforeDatabase(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(SQLImportActivator{}).ActivateImport(ctx,QualifiedImportActivation{});err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
