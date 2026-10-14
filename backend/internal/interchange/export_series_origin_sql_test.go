package interchange

import (
 "context"
 "testing"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestSeriesOriginSQLRequiresInstalledDatabase(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 evidence,err:=source.LookupExportSeriesOrigin(context.Background(),v2input.Entry{Contract:"SeriesCorrections"},[]byte(`{}`))
 if err!=v2input.ErrInvalid||evidence!=(ExportSeriesOriginEvidence{}){t.Fatal("uninstalled origin authority",evidence,err)}
 object,err:=source.LookupExportSeriesOriginDocument(context.Background(),ExportSeriesOriginEvidence{},v2input.Entry{Contract:"SeriesStats"})
 if err!=v2input.ErrInvalid||object!=(objectstore.Object{}){t.Fatal("uninstalled stats authority",object,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.LookupExportSeriesOrigin(ctx,v2input.Entry{},nil);err!=context.Canceled{t.Fatal(err)}
 if _,err:=source.LookupExportSeriesOriginDocument(ctx,ExportSeriesOriginEvidence{},v2input.Entry{});err!=context.Canceled{t.Fatal(err)}
}

func TestSeriesStatImageSQLRequiresInstalledDatabase(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if err:=source.QualifyExportSeriesStatImage(context.Background(),ExportSeriesOriginEvidence{},v2input.Entry{},nil);err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=source.QualifyExportSeriesStatImage(ctx,ExportSeriesOriginEvidence{},v2input.Entry{},nil);err!=context.Canceled{t.Fatal(err)}
}
