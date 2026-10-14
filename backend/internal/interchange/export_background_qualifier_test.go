package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestExportBackgroundQualifierRejectsMissingOrInvalidFrozenAuthority(t *testing.T){
 for _,mode:=range []string{"parent","authority","images","reader","invalid_document","role","cancelled"}{t.Run(mode,func(t *testing.T){
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("{}"))},Contract:"FrozenRenderRecipe"}
  reader:=&exportMaskNodeStub{node:ExportClosureNode{Entry:parent,Body:[]byte("{}")}}
  authority:=&exportBackgroundAuthorityStub{};images:=&exportBinaryQualifierStub{}
  q:=ExportBackgroundQualifier{Reader:reader,Parents:[]v2input.Entry{parent},Authority:authority,Images:images}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))},Contract:"StyleBackgroundAsset"}
  ctx:=context.Background()
  switch mode{case "parent":q.Parents=nil;case "authority":q.Authority=nil;case "images":q.Images=nil;case "reader":q.Reader=nil;case "role":entry.Contract="EncodedImage";case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if err:=q.QualifyExportBinary(ctx,entry,objectstore.Object{});err==nil{t.Fatal("unqualified background accepted")}
  if authority.calls!=0||images.calls!=0{t.Fatal("invalid parent reached background IO")}
 })}
}
func TestExportBackgroundRouterUsesDedicatedQualifier(t *testing.T){
 images,backgrounds:=&exportBinaryQualifierStub{},&exportBinaryQualifierStub{}
 router:=ExportNativeQualifierRouter{Images:images,Backgrounds:backgrounds}
 entry:=v2input.Entry{Contract:"StyleBackgroundAsset"}
 if err:=router.QualifyExportBinary(context.Background(),entry,objectstore.Object{});err!=nil||backgrounds.calls!=1||images.calls!=0{t.Fatal("background bypassed dedicated authority",err)}
 if !exportBinaryContract(entry.Contract){t.Fatal("background treated as JSON")}
 router.Backgrounds=nil
 if err:=router.QualifyExportBinary(context.Background(),entry,objectstore.Object{});err!=v2input.ErrInvalid{t.Fatal("uninstalled background accepted")}
}
