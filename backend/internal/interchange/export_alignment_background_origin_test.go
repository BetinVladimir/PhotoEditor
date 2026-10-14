package interchange
import (
 "context"
 "testing"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestAlignmentBackgroundSQLRequiresLiveContextAndDatabase(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 for _,cancelled:=range []bool{false,true}{
  ctx,cancel:=context.WithCancel(context.Background());if cancelled{cancel()}
  origin,err:=source.LookupExportAlignmentBackgroundOrigin(ctx,ExportBodyDependencyOrigin{},v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{},nil,ExportClosureNode{})
  expected:=error(v2input.ErrInvalid);if cancelled{expected=context.Canceled}
  if err!=expected||origin!=(ExportSeriesMaskOrigin{}){t.Fatal("invalid SQL scope returned evidence",err)}
  object,err:=source.lookupExportAlignmentBackgroundObject(ctx,ExportBodyDependencyOrigin{},v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{},nil,ExportClosureNode{})
  if err!=expected||object!=(objectstore.Object{}){t.Fatal("invalid SQL scope returned object",err)}
  cancel()
 }
}
