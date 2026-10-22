package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestSQLDefectPNGAuthorityRequiresInstallationAndCurrentContext(t *testing.T){
 parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))},Contract:"DefectProposeReport"}
 png:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("png"))},Contract:"DefectLabelPNG"}
 if err:=(SQLExportDocumentCatalog{}).QualifyExportDefectPNG(context.Background(),parent,[]byte(`{}`),png);!errors.Is(err,v2input.ErrInvalid){t.Fatal("missing scoped SQL accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(SQLExportDocumentCatalog{}).QualifyExportDefectPNG(ctx,parent,[]byte(`{}`),png);!errors.Is(err,context.Canceled){t.Fatal("cancelled authority proceeded",err)}
 if err:=(SQLExportResultInputCatalog{}).QualifyExportDefectPNG(context.Background(),parent,[]byte(`{}`),png);err==nil{t.Fatal("unbound result source accepted")}
}
