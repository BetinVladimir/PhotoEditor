package interchange

import (
 "bytes"
 "encoding/json"
 "io"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// NPY v2: eight magic/version bytes, four length bytes, bounded header.
const exportNPYOverhead int64 = 12 + 4096

type ExportArrayGeometry struct{Width,Height int;CanvasSHA,DType string}
type ExportNativeArrayReceipt struct{
 Schema int `json:"schema_version"`
 Contract string `json:"contract"`
 SHA string `json:"sha256"`
 Bytes int64 `json:"bytes"`
 Media string `json:"media_type"`
 Width int `json:"width"`
 Height int `json:"height"`
 DType string `json:"dtype"`
 Canvas string `json:"canvas_hash"`
 Runtime string `json:"runtime_sha256"`
}
func exportArrayMediaType(media string)bool{return media=="application/octet-stream"||media=="application/x-npy"}
func exportArrayBinaryContract(contract string)bool{
 switch contract{case "WorkingPixels","PixelArray","WorkingAlpha","DenseWarpField","DenseWarpArray","AbsoluteLUTArray":return true};return false
}
func exportArrayMaximumBytes(entry v2input.Entry,g ExportArrayGeometry)(int64,error){
 if !exportArrayBinaryContract(entry.Contract)||!validImportRef(entry.Ref)||!validExportMaskGeometry(ExportMaskGeometry{Width:g.Width,Height:g.Height,CanvasSHA:g.CanvasSHA},entry.Ref)||(g.DType!="float32"&&g.DType!="uint16"){return 0,v2input.ErrInvalid}
 channels,itemBytes:=int64(1),int64(4)
 switch entry.Contract{
 case "AbsoluteLUTArray":
  if (g.Width!=33&&g.Width!=65)||g.Height!=g.Width||g.DType!="float32"{return 0,v2input.ErrInvalid};channels=3*int64(g.Width)
 case "WorkingPixels","PixelArray":channels=3;if g.DType=="uint16"{itemBytes=2}
 case "DenseWarpField","DenseWarpArray":channels=2;if g.DType!="float32"{return 0,v2input.ErrInvalid}
 }
 return int64(g.Width)*int64(g.Height)*channels*itemBytes+exportNPYOverhead,nil
}

// The receipt is accepted only against independently resolved typed geometry
// and deployment runtime pin. Alpha retains the legacy dual physical encoding.
func decodeExportNativeArrayReceipt(raw []byte,entry v2input.Entry,object objectstore.Object,g ExportArrayGeometry,runtimeSHA string)(ExportNativeArrayReceipt,error){
 var zero ExportNativeArrayReceipt
 maximum,err:=exportArrayMaximumBytes(entry,g);if err!=nil{return zero,err}
 if len(raw)<2||len(raw)>4096||!validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:runtimeSHA}){return zero,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return zero,v2input.ErrInvalid}
 var receipt ExportNativeArrayReceipt
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&receipt)!=nil||decoder.Decode(new(any))!=io.EOF{return zero,v2input.ErrInvalid}
 encoded,err:=canonical.Marshal(receipt);if err!=nil||!bytes.Equal(encoded,raw){return zero,v2input.ErrInvalid}
 dtypeOK:=receipt.DType==g.DType
 if entry.Contract=="WorkingAlpha"{dtypeOK=receipt.DType=="float32"||receipt.DType=="uint16"}
 if receipt.Schema!=1||receipt.Contract!=entry.Contract||receipt.SHA!=entry.Ref.SHA256||receipt.SHA!=object.SHA256||receipt.Bytes!=object.Bytes||receipt.Bytes<1||receipt.Bytes>maximum||!exportArrayMediaType(receipt.Media)||receipt.Media!=object.ContentType||receipt.Width!=g.Width||receipt.Height!=g.Height||receipt.Canvas!=g.CanvasSHA||receipt.Runtime!=runtimeSHA||!dtypeOK{return zero,v2input.ErrInvalid}
 return receipt,nil
}
