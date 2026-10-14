package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestStatsPersonDocumentSQLCatalogRequiresInstallationAndScope(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("{}"))}}
 origin:=ExportStatsPersonOrigin{Root:ref(),Params:ref(),Report:ref(),Mask:objectstore.Object{Key:"mask",VersionID:"immutable",SHA256:rawSHA([]byte("mask")),Bytes:4,ContentType:"application/x-npy"}}
 for _,mode:=range []string{"missing-db","contract","ref","cancelled"}{t.Run(mode,func(t *testing.T){
  entry:=v2input.Entry{Ref:origin.Root,Contract:"MaskRef"};ctx:=context.Background()
  switch mode{case "contract":entry.Contract="Params";case "ref":entry.Ref=ref();case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  result,err:=(SQLExportDocumentCatalog{}).LookupExportStatsPersonDocument(ctx,origin,entry)
  if err==nil||result!=(objectstore.Object{}){t.Fatal("uninstalled or unscoped catalogue admitted original document",err)}
 })}
}
