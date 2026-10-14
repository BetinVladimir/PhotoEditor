package interchange
import (
 "context"
 "os"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestPhysicalSourceRequiresIndependentNativeAuthority(t *testing.T){
 r:=ExportPhysicalSourceResolver{}
 if _,err:=r.ResolveExportNode(context.Background(),v2input.Entry{Contract:"WorkingImage"});err==nil{t.Fatal("metadata-only source accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=r.ResolveExportNode(ctx,v2input.Entry{});err!=context.Canceled{t.Fatal("cancellation lost",err)}
}

func TestPhysicalSourceRequiresExactArrayInventory(t *testing.T){
 for _,mode:=range []string{"valid","missing","duplicate","foreign","wrong_contract","alpha_alias"}{t.Run(mode,func(t *testing.T){
  _,_,raw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  image.Alpha=nil
  pixels:=v2input.Entry{Contract:"WorkingPixels",Ref:v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256}}
  children:=[]v2input.Entry{pixels}
  switch mode{
  case "missing":children=nil
  case "duplicate":children=append(children,pixels)
  case "foreign":children[0].Ref.ArtifactID=uuid.New()
  case "wrong_contract":children[0].Contract="WorkingAlpha"
  case "alpha_alias":alpha:=image.Pixels;image.Alpha=&alpha
  }
  err=validateExportPhysicalSourceChildren(image,children)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected array inventory decision",err)}
 })}
}

type physicalWorkingStub struct{calls int;deny bool;denyAt int}
func(s *physicalWorkingStub)QualifyExportWorkingArray(context.Context,v2input.Entry,v2input.Entry)error{s.calls++;if s.deny||s.calls==s.denyAt{return v2input.ErrInvalid};return nil}
type physicalCatalogStub struct{object objectstore.Object;calls int;change bool}
func(s *physicalCatalogStub)LookupExportDocument(context.Context,v2input.Entry)(objectstore.Object,error){s.calls++;o:=s.object;if s.change&&s.calls>1{o.VersionID="replaced"};return o,nil}
func TestPhysicalSourceNativeLifecycle(t *testing.T){
 for _,mode:=range []string{"valid","authority","final_authority","object_changed","hash","probe","missing_pixels","cancelled"}{t.Run(mode,func(t *testing.T){
  _,_,raw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  binary:=[]byte("pixels");image.Pixels=review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA(binary)};image.Alpha=nil
  raw,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Contract:"WorkingImage",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  pixels:=v2input.Entry{Contract:"WorkingPixels",Ref:v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256}}
  reader:=&exportMaskNodeStub{node:ExportClosureNode{Entry:parent,Body:raw,Children:[]v2input.Entry{pixels}}}
  catalog:=&physicalCatalogStub{object:objectstore.Object{Key:"pixels",VersionID:"v1",SHA256:pixels.Ref.SHA256,Bytes:int64(len(binary)),ContentType:"application/octet-stream"},change:mode=="object_changed"}
  objects:=&importReaderStub{body:binary};working:=&physicalWorkingStub{deny:mode=="authority"}
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,1<<20);if err!=nil{t.Fatal(err)}
  runtime:=rawSHA([]byte("array runtime"));probe:=&exportArrayProbeStub{runtime:runtime}
  q:=ExportPhysicalSourceResolver{JSON:reader,Catalog:catalog,Objects:objects,Working:working,Warps:&exportWarpAuthorityStub{},Scratch:scratch,Probe:probe,RuntimeSHA:runtime}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{case "final_authority":working.denyAt=3;case "hash":objects.body=[]byte("changed");case "probe":probe.err=v2input.ErrInvalid;case "missing_pixels":reader.node.Children=nil;case "cancelled":cancel()}
  node,err:=q.ResolveExportNode(ctx,parent)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected physical source decision",err)}
  if mode=="valid"&&(node.Entry!=parent||rawSHA(node.Body)!=parent.Ref.SHA256||probe.calls!=1||working.calls!=3||catalog.calls!=2){t.Fatal("incomplete physical lifecycle")}
  if (mode=="missing_pixels"||mode=="cancelled")&&(probe.calls!=0||objects.opens!=0){t.Fatal("invalid inventory reached binary IO")}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",err)}
 })}
}
