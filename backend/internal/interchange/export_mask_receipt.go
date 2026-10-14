package interchange

import (
 "bytes"
 "encoding/json"
 "io"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type ExportMaskGeometry struct{Width,Height int;CanvasSHA string}
type ExportNativeMaskReceipt struct{
 Schema int `json:"schema_version"`
 SHA string `json:"sha256"`
 Bytes int64 `json:"bytes"`
 Media string `json:"media_type"`
 Width int `json:"width"`
 Height int `json:"height"`
 DType string `json:"dtype"`
 Canvas string `json:"canvas_hash"`
 Runtime string `json:"runtime_sha256"`
}
// Geometry must originate from the resolved typed canvas, never a binary header.
// RuntimeSHA must be independently qualified by deployment admission.
func decodeExportNativeMaskReceipt(raw []byte,entry v2input.Entry,object objectstore.Object,geometry ExportMaskGeometry,runtimeSHA string)(ExportNativeMaskReceipt,error){
 var zero ExportNativeMaskReceipt
 if len(raw)<2||len(raw)>4096||!exportMaskBinaryContract(entry.Contract)||!validImportRef(entry.Ref)||
  !validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:runtimeSHA})||
  !validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:geometry.CanvasSHA})||
  geometry.Width<1||geometry.Height<1||geometry.Width>200000000||geometry.Height>200000000||int64(geometry.Width)*int64(geometry.Height)>200000000{return zero,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return zero,v2input.ErrInvalid}
 var receipt ExportNativeMaskReceipt
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&receipt)!=nil||decoder.Decode(new(any))!=io.EOF{return zero,v2input.ErrInvalid}
 encoded,err:=canonical.Marshal(receipt);if err!=nil||!bytes.Equal(encoded,raw){return zero,v2input.ErrInvalid}
 if receipt.Schema!=1||receipt.SHA!=entry.Ref.SHA256||receipt.SHA!=object.SHA256||receipt.Bytes!=object.Bytes||receipt.Bytes<1||receipt.Bytes>int64(geometry.Width)*int64(geometry.Height)*4+exportNPYOverhead||!exportArrayMediaType(receipt.Media)||receipt.Media!=object.ContentType||receipt.Width!=geometry.Width||receipt.Height!=geometry.Height||receipt.Canvas!=geometry.CanvasSHA||receipt.Runtime!=runtimeSHA||receipt.DType!="float32"{return zero,v2input.ErrInvalid}
 return receipt,nil
}
