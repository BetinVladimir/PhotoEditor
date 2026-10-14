package interchange
import (
 "bytes"
 "encoding/json"
 "io"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type ExportNativeImageReceipt struct{
 Schema int `json:"schema_version"`
 SHA string `json:"sha256"`
 Bytes int64 `json:"bytes"`
 Media string `json:"media_type"`
 Width int `json:"width"`
 Height int `json:"height"`
 BitDepth int `json:"bit_depth"`
 Alpha *bool `json:"has_alpha"`
 Orientation int `json:"orientation"`
 ICC *string `json:"icc_sha256"`
 Runtime string `json:"runtime_sha256"`
}
// Runtime pin must come from an independently qualified installed deployment.
// A syntactically correct receipt or self-declared version is not qualification.
func decodeExportNativeImageReceipt(raw []byte,entry v2input.Entry,object objectstore.Object,runtimeSHA string)(ExportNativeImageReceipt,error){
 var zero ExportNativeImageReceipt
 if len(raw)<2||len(raw)>4096||entry.Contract!="EncodedImage"||!validImportRef(entry.Ref)||!validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:runtimeSHA}){return zero,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return zero,v2input.ErrInvalid}
 var receipt ExportNativeImageReceipt
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&receipt)!=nil||decoder.Decode(new(any))!=io.EOF{return zero,v2input.ErrInvalid}
 encoded,err:=canonical.Marshal(receipt);if err!=nil||!bytes.Equal(encoded,raw){return zero,v2input.ErrInvalid}
 if receipt.Schema!=1||receipt.SHA!=entry.Ref.SHA256||receipt.SHA!=object.SHA256||receipt.Bytes!=object.Bytes||receipt.Bytes<1||receipt.Bytes>64<<30||receipt.Media!=object.ContentType||receipt.Runtime!=runtimeSHA||receipt.Alpha==nil||receipt.Width<1||receipt.Height<1||receipt.Width>200000000||receipt.Height>200000000||int64(receipt.Width)*int64(receipt.Height)>200000000||receipt.Orientation<1||receipt.Orientation>8||(receipt.BitDepth!=8&&receipt.BitDepth!=16){return zero,v2input.ErrInvalid}
 if _,err:=exportOriginalSuffix(receipt.Media);err!=nil{return zero,err}
 if receipt.ICC!=nil&&!validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:*receipt.ICC}){return zero,v2input.ErrInvalid}
 return receipt,nil
}
