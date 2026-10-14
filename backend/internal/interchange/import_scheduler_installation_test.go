package interchange
import (
 "context"
 "testing"
)
func TestImportSchedulerInstallationRejectsMissingPool(t *testing.T){
 if err:=ValidateSQLImportContinuationInstallation(context.Background(),nil);err==nil{t.Fatal("missing scheduler pool accepted")}
}
func TestImportSchedulerInstallationCancellationBeforeDatabase(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=ValidateSQLImportContinuationInstallation(ctx,nil);err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
