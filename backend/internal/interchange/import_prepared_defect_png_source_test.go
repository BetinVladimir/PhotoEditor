package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestPreparedPNGSourceRequiresCurrentSQLAndIndependentAuthorities(t *testing.T){
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("png"))},Contract:"DefectLabelPNG"}
 if inputs,err:=(SQLPreparedDefectPNGSource{}).ResolveExportDefectPNG(context.Background(),entry);err==nil||inputs!=nil{t.Fatal("missing prepared authority accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if inputs,err:=(SQLPreparedDefectPNGSource{}).ResolveExportDefectPNG(ctx,entry);!errors.Is(err,context.Canceled)||inputs!=nil{t.Fatal("cancellation lost",err)}
}

func TestPreparedPNGResolverRechecksReceiptAfterPhysicalQualification(t *testing.T){
 for _,mode:=range []string{"valid","changed","semantic_failure","missing_qualifier","wrong_media","wrong_hash"}{t.Run(mode,func(t *testing.T){
  entry,catalog,_:=importOpaqueFixture();entry.Contract="DefectLabelPNG"
  catalog.object.ContentType="image/png";catalog.object.Bytes=100
  native:=&exportBinaryQualifierStub{};var qualifier ExportBinaryQualifier=native
  switch mode{case "changed":catalog.change=true;case "semantic_failure":native.fail=true;case "missing_qualifier":qualifier=nil;case "wrong_media":catalog.object.ContentType="application/octet-stream";case "wrong_hash":catalog.object.SHA256=rawSHA([]byte("foreign"))}
  node,err:=(ImportPreparedPNGNodeResolver{Catalog:catalog,Qualifier:qualifier}).ResolveImportNode(context.Background(),entry)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected PNG receipt result",err)}
  if mode=="valid"&&(node.Entry!=entry||node.Body!=nil||len(node.Children)!=0||native.calls!=1||catalog.calls!=2){t.Fatal("PNG qualification/recheck omitted")}
  if mode=="missing_qualifier"&&catalog.calls!=0{t.Fatal("uninstalled authority performed IO")}
 })}
}

func TestPreparedPNGExactTypedParentEdge(t *testing.T){
 required:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("png"))},Contract:"DefectLabelPNG"}
 foreign:=required;foreign.Contract="Artifact"
 for _,children:=range [][]v2input.Entry{nil,{foreign},{required,required}}{if preparedPNGChild(children,required){t.Fatal("missing, ambiguous or untyped PNG edge accepted")}}
 if !preparedPNGChild([]v2input.Entry{required},required){t.Fatal("exact edge rejected")}
}
