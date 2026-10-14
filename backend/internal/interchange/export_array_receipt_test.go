package interchange

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestExportArrayReceiptContractDTypeAndPins(t *testing.T){
 for _,contract:=range []string{"WorkingPixels","PixelArray","WorkingAlpha","DenseWarpField","DenseWarpArray"}{
 for _,mode:=range []string{"valid","npy","media","media_substitution","contract","sha","size","canvas","runtime","geometry","dtype","unknown","noncanonical"}{t.Run(contract+"/"+mode,func(t *testing.T){
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("array"))},Contract:contract}
  object:=objectstore.Object{SHA256:entry.Ref.SHA256,Bytes:140,ContentType:"application/octet-stream"}
  g:=ExportArrayGeometry{Width:3,Height:1,CanvasSHA:rawSHA([]byte("canvas")),DType:"float32"};runtime:=rawSHA([]byte("runtime"))
  receipt:=ExportNativeArrayReceipt{Schema:1,Contract:contract,SHA:entry.Ref.SHA256,Bytes:140,Media:object.ContentType,Width:3,Height:1,DType:"float32",Canvas:g.CanvasSHA,Runtime:runtime}
  switch mode{case "npy":object.ContentType="application/x-npy";receipt.Media=object.ContentType;case "media":object.ContentType="application/json";receipt.Media=object.ContentType;case "media_substitution":receipt.Media="application/x-npy";case "contract":receipt.Contract="MaskPixels";case "sha":receipt.SHA=rawSHA([]byte("other"));case "size":receipt.Bytes++;case "canvas":receipt.Canvas=runtime;case "runtime":receipt.Runtime=g.CanvasSHA;case "geometry":receipt.Width++;case "dtype":receipt.DType="float64"}
  raw,err:=canonical.Marshal(receipt);if err!=nil{t.Fatal(err)}
  if mode=="unknown"{raw=append([]byte(`{"approved":true,`),raw[1:]...)}
  if mode=="noncanonical"{raw=append(raw,'\n')}
  _,err=decodeExportNativeArrayReceipt(raw,entry,object,g,runtime)
  if (err==nil)!=(mode=="valid"||mode=="npy"){t.Fatalf("unexpected receipt result: %v",err)}
 })}}
}

func TestExportArrayReceiptLegacyAlphaAndUint16RGB(t *testing.T){
 for _,contract:=range []string{"WorkingAlpha","WorkingPixels"}{
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("legacy"))},Contract:contract}
  g:=ExportArrayGeometry{Width:3,Height:1,CanvasSHA:rawSHA([]byte("canvas")),DType:"uint16"}
  if contract=="WorkingAlpha"{g.DType="float32"}
  runtime:=rawSHA([]byte("runtime"));object:=objectstore.Object{SHA256:entry.Ref.SHA256,Bytes:140,ContentType:"application/octet-stream"}
  raw,err:=canonical.Marshal(ExportNativeArrayReceipt{Schema:1,Contract:contract,SHA:entry.Ref.SHA256,Bytes:140,Media:object.ContentType,Width:3,Height:1,DType:"uint16",Canvas:g.CanvasSHA,Runtime:runtime});if err!=nil{t.Fatal(err)}
  if _,err:=decodeExportNativeArrayReceipt(raw,entry,object,g,runtime);err!=nil{t.Fatal(err)}
 }
}


func TestExportArrayMaximumIncludesNPYVersionTwoPrefix(t *testing.T){
 for _,item:=range []struct{contract string;channels int64}{{"WorkingPixels",3},{"WorkingAlpha",1},{"DenseWarpField",2}}{
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("array"))},Contract:item.contract}
  geometry:=ExportArrayGeometry{Width:2,Height:1,CanvasSHA:rawSHA([]byte("canvas")),DType:"float32"}
  maximum,err:=exportArrayMaximumBytes(entry,geometry);if err!=nil{t.Fatal(err)}
  expected:=int64(2)*item.channels*4+4096+12
  if maximum!=expected{t.Fatal("NPY preamble excluded",maximum,expected)}
  object:=objectstore.Object{SHA256:entry.Ref.SHA256,Bytes:expected,ContentType:"application/x-npy"}
  runtime:=rawSHA([]byte("runtime"))
  receipt:=ExportNativeArrayReceipt{Schema:1,Contract:item.contract,SHA:entry.Ref.SHA256,Bytes:expected,Media:object.ContentType,Width:2,Height:1,DType:"float32",Canvas:geometry.CanvasSHA,Runtime:runtime}
  raw,err:=canonical.Marshal(receipt);if err!=nil{t.Fatal(err)}
  if _,err:=decodeExportNativeArrayReceipt(raw,entry,object,geometry,runtime);err!=nil{t.Fatal(err)}
  object.Bytes++;receipt.Bytes++;raw,err=canonical.Marshal(receipt);if err!=nil{t.Fatal(err)}
  if _,err:=decodeExportNativeArrayReceipt(raw,entry,object,geometry,runtime);err==nil{t.Fatal("oversize NPY accepted")}
 }
}

func TestAbsoluteLUTArrayBoundsRequireExactGrid(t *testing.T) {
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("lut"))},Contract:"AbsoluteLUTArray"}
 for _,size:=range []int{33,65} {
  geometry:=ExportArrayGeometry{Width:size,Height:size,CanvasSHA:rawSHA([]byte("resolved")),DType:"float32"}
  maximum,err:=exportArrayMaximumBytes(entry,geometry)
  if err!=nil||maximum!=int64(size)*int64(size)*int64(size)*3*4+exportNPYOverhead {t.Fatal("LUT bound",maximum,err)}
  geometry.Height--
  if _,err:=exportArrayMaximumBytes(entry,geometry);err==nil {t.Fatal("noncubic LUT accepted")}
 }
}
