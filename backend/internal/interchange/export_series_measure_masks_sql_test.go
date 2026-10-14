package interchange

import (
 "context"
 "testing"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestSeriesMeasureMaskSQLRequiresDatabaseAndHonorsCancellation(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 object,err:=source.LookupExportSeriesMeasureMask(context.Background(),ExportSeriesMeasureOrigin{},v2input.Entry{},nil,ExportClosureNode{},v2input.Ref{})
 if err!=v2input.ErrInvalid||object!=(objectstore.Object{}){t.Fatal("uninstalled mask authority accepted",object,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.LookupExportSeriesMeasureMask(ctx,ExportSeriesMeasureOrigin{},v2input.Entry{},nil,ExportClosureNode{},v2input.Ref{});err!=context.Canceled{t.Fatal(err)}
}
