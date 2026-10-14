package interchange

import (
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestExportMaskReceiptExactPins(t *testing.T){
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},Contract:"MaskArray"}
 object:=objectstore.Object{SHA256:entry.Ref.SHA256,Bytes:140,ContentType:"application/octet-stream"}
 geometry:=ExportMaskGeometry{Width:3,Height:1,CanvasSHA:strings.Repeat("b",64)}
 runtime:=strings.Repeat("c",64)
 for _,mode:=range []string{"valid","npy","media","media_substitution","mask_pixels","sha","size","width","canvas","runtime","dtype","unknown","noncanonical","role"}{t.Run(mode,func(t *testing.T){
  local:=entry
  object:=object
  if mode=="mask_pixels"{local.Contract="MaskPixels"}
  receipt:=ExportNativeMaskReceipt{Schema:1,SHA:entry.Ref.SHA256,Bytes:140,Media:object.ContentType,Width:3,Height:1,DType:"float32",Canvas:geometry.CanvasSHA,Runtime:runtime}
  switch mode{case "npy":object.ContentType="application/x-npy";receipt.Media=object.ContentType;case "media":object.ContentType="application/json";receipt.Media=object.ContentType;case "media_substitution":receipt.Media="application/x-npy";case "sha":receipt.SHA=strings.Repeat("d",64);case "size":receipt.Bytes++;case "width":receipt.Width++;case "canvas":receipt.Canvas=strings.Repeat("d",64);case "runtime":receipt.Runtime=strings.Repeat("d",64);case "dtype":receipt.DType="uint16";case "role":local.Contract="WorkingPixels"}
  raw,err:=canonical.Marshal(receipt);if err!=nil{t.Fatal(err)}
  if mode=="unknown"{raw=append([]byte(`{"approved":true,`),raw[1:]...)}
  if mode=="noncanonical"{raw=append(raw,'\n')}
  _,err=decodeExportNativeMaskReceipt(raw,local,object,geometry,runtime)
  if (err==nil)!=(mode=="valid"||mode=="mask_pixels"||mode=="npy"){t.Fatalf("unexpected receipt result: %v",err)}
 })}
}
