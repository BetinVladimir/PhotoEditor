package interchange
import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestPairedPersonRequiresOriginalFitAuthority(t *testing.T){
 q:=NativeExportBodyFitPairedPersons{}
 if err:=q.QualifyExportBodyFitPairedPersons(context.Background(),v2input.Entry{},nil,ExportClosureNode{});err==nil{t.Fatal("missing original authority accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=q.QualifyExportBodyFitPairedPersons(ctx,v2input.Entry{},nil,ExportClosureNode{});err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
