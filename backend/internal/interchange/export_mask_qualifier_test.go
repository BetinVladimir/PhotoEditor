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

type exportMaskGeometryStub struct{geometry ExportMaskGeometry;calls int;change bool;err error}
func(s *exportMaskGeometryStub)ResolveExportMaskGeometry(context.Context,v2input.Ref)(ExportMaskGeometry,error){
 s.calls++;g:=s.geometry;if s.change&&s.calls>1{g.Width++};return g,s.err
}
type exportMaskProbeStub struct{runtime,path string;calls int;err error}
func(s *exportMaskProbeStub)RunExportNativeProbe(_ context.Context,raw []byte)([]byte,error){
 s.calls++
 var request struct{Path string `json:"file_path"`;SHA string `json:"sha256"`;Bytes int64 `json:"bytes"`;Media string `json:"media_type"`;Width int `json:"width"`;Height int `json:"height"`;Canvas string `json:"canvas_hash"`}
 if err:=json.Unmarshal(raw,&request);err!=nil{return nil,err};s.path=request.Path
 if _,err:=os.Stat(request.Path);err!=nil{return nil,err};if s.err!=nil{return nil,s.err}
 return canonical.Marshal(ExportNativeMaskReceipt{Schema:1,SHA:request.SHA,Bytes:request.Bytes,Media:request.Media,Width:request.Width,Height:request.Height,Canvas:request.Canvas,DType:"float32",Runtime:s.runtime})
}

// Mocked probe exercises staging/receipt binding, not actual NPY qualification.
func TestExportMaskQualifierStagesRechecksAndCleans(t *testing.T){
 for _,mode:=range []string{"valid","npy","media","mask_pixels","hash","size","budget","geometry","changed","probe","missing_geometry","role","cancelled"}{t.Run(mode,func(t *testing.T){
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,100);if err!=nil{t.Fatal(err)}
  body:=[]byte("mask");entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Contract:"MaskArray"}
  object:=objectstore.Object{Key:"mask",VersionID:"v1",SHA256:entry.Ref.SHA256,Bytes:4,ContentType:"application/octet-stream"}
  objects:=&importReaderStub{body:body};runtime:=rawSHA([]byte("runtime"));cause:=errors.New("qualification refused")
  geometry:=&exportMaskGeometryStub{geometry:ExportMaskGeometry{Width:2,Height:1,CanvasSHA:rawSHA([]byte("canvas"))}}
  probe:=&exportMaskProbeStub{runtime:runtime}
  q:=NativeExportMaskQualifier{Objects:objects,Scratch:scratch,Probe:probe,Geometry:geometry,RuntimeSHA:runtime}
  ctx:=context.Background()
  switch mode{case "npy":object.ContentType="application/x-npy";case "media":object.ContentType="application/json";case "mask_pixels":entry.Contract="MaskPixels";case "hash":objects.body=[]byte("oops");case "size":object.Bytes++;case "budget":scratch.maximum=4;case "geometry":geometry.err=cause;case "changed":geometry.change=true;case "probe":probe.err=cause;case "missing_geometry":q.Geometry=nil;case "role":entry.Contract="WorkingPixels";case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  err=q.QualifyExportBinary(ctx,entry,object)
  valid:=mode=="valid"||mode=="mask_pixels"||mode=="npy"
  if (err==nil)!=valid{t.Fatalf("unexpected qualification: %v",err)}
  if valid&&(probe.calls!=1||geometry.calls!=2){t.Fatal("missing probe or geometry recheck")}
  if (mode=="geometry"||mode=="probe")&&!errors.Is(err,cause){t.Fatal("cause lost")}
  files,readErr:=os.ReadDir(root);if readErr!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",readErr,scratch.used)}
 })}
}

func TestExportNativeRouterClosedContractDispatch(t *testing.T){
 images,masks:=&exportBinaryQualifierStub{},&exportBinaryQualifierStub{}
 router:=ExportNativeQualifierRouter{Images:images,Masks:masks}
 for _,contract:=range []string{"EncodedImage","MaskArray","WorkingPixels","DenseWarpArray","MaskPixels"}{
  err:=router.QualifyExportBinary(context.Background(),v2input.Entry{Contract:contract},objectstore.Object{})
  supported:=contract=="EncodedImage"||contract=="MaskArray"||contract=="MaskPixels"
  if (err==nil)!=supported{t.Fatal("unexpected dispatch",contract,err)}
 }
 if images.calls!=1||masks.calls!=2{t.Fatal("incorrect dispatch counts")}
 if err:=(ExportNativeQualifierRouter{}).QualifyExportBinary(context.Background(),v2input.Entry{Contract:"MaskArray"},objectstore.Object{});err!=v2input.ErrInvalid{t.Fatal("missing qualifier accepted")}
}
