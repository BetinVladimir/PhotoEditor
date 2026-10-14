package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestResolvedOriginRequiresInstalledScopedCatalogue(t *testing.T){
 for _,mode:=range []string{"missing-db","contract","hash","cancelled"}{t.Run(mode,func(t *testing.T){
  raw:=[]byte("{}");entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"ResolvedStyle"};ctx:=context.Background()
  switch mode{case "contract":entry.Contract="StyleV2";case "hash":entry.Ref.SHA256=rawSHA([]byte("different"));case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  result,err:=(SQLExportDocumentCatalog{}).LookupExportResolvedOrigin(ctx,entry,raw)
  if err==nil||result!=(ExportResolvedOriginEvidence{}){t.Fatal("uninstalled resolved origin admitted",err)}
 })}
}
