package interchange

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type exportArrayGeometryStub struct{geometry ExportArrayGeometry;calls int;change bool;err error}
func(s *exportArrayGeometryStub)ResolveExportArrayGeometry(context.Context,v2input.Entry)(ExportArrayGeometry,error){
 s.calls++;g:=s.geometry;if s.change&&s.calls>1{g.Width++};return g,s.err
}
type exportArrayProbeStub struct{runtime,path string;calls int;err error}
func(s *exportArrayProbeStub)RunExportNativeProbe(_ context.Context,raw []byte)([]byte,error){
 s.calls++
 var request struct{Path string `json:"file_path"`;SHA string `json:"sha256"`;Bytes int64 `json:"bytes"`;Media string `json:"media_type"`;Width int `json:"width"`;Height int `json:"height"`;Canvas string `json:"canvas_hash"`;Contract string `json:"contract"`;DType string `json:"dtype"`}
 if err:=json.Unmarshal(raw,&request);err!=nil{return nil,err};s.path=request.Path
 if _,err:=os.Stat(request.Path);err!=nil{return nil,err};if s.err!=nil{return nil,s.err}
 return canonical.Marshal(ExportNativeArrayReceipt{Schema:1,Contract:request.Contract,SHA:request.SHA,Bytes:request.Bytes,Media:request.Media,Width:request.Width,Height:request.Height,Canvas:request.Canvas,DType:request.DType,Runtime:s.runtime})
}

// Mocked probe exercises staging/receipt binding, not actual NPY qualification.
func TestExportArrayQualifierStagesRechecksAndCleans(t *testing.T){
 for _,mode:=range []string{"valid","npy","media","warp","hash","size","budget","geometry","changed","probe","missing_geometry","role","cancelled"}{t.Run(mode,func(t *testing.T){
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,100);if err!=nil{t.Fatal(err)}
  body:=[]byte("mask");entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Contract:"WorkingPixels"}
  object:=objectstore.Object{Key:"mask",VersionID:"v1",SHA256:entry.Ref.SHA256,Bytes:4,ContentType:"application/octet-stream"}
  objects:=&importReaderStub{body:body};runtime:=rawSHA([]byte("runtime"));cause:=errors.New("qualification refused")
  geometry:=&exportArrayGeometryStub{geometry:ExportArrayGeometry{Width:2,Height:1,CanvasSHA:rawSHA([]byte("canvas")),DType:"float32"}}
  probe:=&exportArrayProbeStub{runtime:runtime}
  q:=NativeExportArrayQualifier{Objects:objects,Scratch:scratch,Probe:probe,Geometry:geometry,RuntimeSHA:runtime}
  ctx:=context.Background()
  switch mode{case "npy":object.ContentType="application/x-npy";case "media":object.ContentType="application/json";case "warp":entry.Contract="DenseWarpField";case "hash":objects.body=[]byte("oops");case "size":object.Bytes++;case "budget":scratch.maximum=4;case "geometry":geometry.err=cause;case "changed":geometry.change=true;case "probe":probe.err=cause;case "missing_geometry":q.Geometry=nil;case "role":entry.Contract="Artifact";case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  err=q.QualifyExportBinary(ctx,entry,object)
  valid:=mode=="valid"||mode=="warp"||mode=="npy"
  if (err==nil)!=valid{t.Fatalf("unexpected qualification: %v",err)}
  if valid&&(probe.calls!=1||geometry.calls!=2){t.Fatal("missing probe or geometry recheck")}
  if (mode=="geometry"||mode=="probe")&&!errors.Is(err,cause){t.Fatal("cause lost")}
  files,readErr:=os.ReadDir(root);if readErr!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",readErr,scratch.used)}
 })}
}

func TestExportNativeRouterArrayDispatch(t *testing.T){
 images,masks,arrays:=&exportBinaryQualifierStub{},&exportBinaryQualifierStub{},&exportBinaryQualifierStub{}
 router:=ExportNativeQualifierRouter{Images:images,Masks:masks,Arrays:arrays}
 for _,contract:=range []string{"WorkingPixels","PixelArray","WorkingAlpha","DenseWarpField","DenseWarpArray"}{
  if err:=router.QualifyExportBinary(context.Background(),v2input.Entry{Contract:contract},objectstore.Object{});err!=nil{t.Fatal(contract,err)}
 }
 if arrays.calls!=5||images.calls!=0||masks.calls!=0{t.Fatal("array contracts crossed qualifier boundary")}
 if err:=router.QualifyExportBinary(context.Background(),v2input.Entry{Contract:"NpyArray"},objectstore.Object{});err!=v2input.ErrInvalid{t.Fatal("untyped array accepted")}
}
