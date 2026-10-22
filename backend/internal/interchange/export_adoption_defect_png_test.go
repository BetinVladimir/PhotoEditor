package interchange
import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestSchedulerDefectPNGRequiresCurrentClaimInstallation(t *testing.T){
 catalog:=SQLSchedulerExportInputCatalog{}
 if err:=catalog.QualifyExportDefectPNG(context.Background(),v2input.Entry{},nil,v2input.Entry{});err==nil{t.Fatal("uninstalled claim authority accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=catalog.QualifyExportDefectPNG(ctx,v2input.Entry{},nil,v2input.Entry{});!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
}
