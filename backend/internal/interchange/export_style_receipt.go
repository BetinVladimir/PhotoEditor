package interchange

import (
 "bytes"
 "encoding/json"
 "io"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type ExportNativeStyleReceipt struct{
 Schema int `json:"schema_version"`
 Contract string `json:"contract"`
 SHA string `json:"sha256"`
 Bytes int64 `json:"bytes"`
 Policy string `json:"model_policy_sha256"`
 Runtime string `json:"runtime_sha256"`
}
func decodeExportNativeStyleReceipt(raw []byte,entry v2input.Entry,size int64,policy,runtime string)(ExportNativeStyleReceipt,error){
 var zero ExportNativeStyleReceipt
 maximum:=int64(8<<20);if entry.Contract=="BodyFitOrigin"{maximum=16<<20};if (entry.Contract=="BodyDependencyOrigin"||entry.Contract=="PairAlignmentOrigin"){maximum=4<<20};if entry.Contract=="BodyOrigin"{maximum=10<<20};if entry.Contract=="MaskOrigin"{maximum=12<<20};if entry.Contract=="SeriesMeasureOrigin"{maximum=10<<20};if entry.Contract=="SeriesOrigin"{maximum=32<<20};if entry.Contract=="OperationParams"{maximum=2<<20};if entry.Contract=="ResolvedOrigin"{maximum=10<<20};if entry.Contract=="RenderComposition"{maximum=128<<20}
 if len(raw)<2||len(raw)>4096||(entry.Contract!="StyleV2"&&entry.Contract!="Override"&&entry.Contract!="RenderComposition"&&entry.Contract!="ResolvedStyle"&&entry.Contract!="ResolvedOrigin"&&entry.Contract!="OperationParams"&&entry.Contract!="SeriesCorrections"&&entry.Contract!="SeriesOrigin"&&entry.Contract!="SeriesMeasureOrigin"&&entry.Contract!="MaskOrigin"&&entry.Contract!="BodyOrigin"&&entry.Contract!="BodyDependencyOrigin"&&entry.Contract!="BodyFitOrigin"&&entry.Contract!="PairAlignmentOrigin"&&entry.Contract!="FaceDetectorOrigin")||!validImportRef(entry.Ref)||size<2||size>maximum||!validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:policy})||!validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:runtime}){return zero,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return zero,v2input.ErrInvalid}
 var receipt ExportNativeStyleReceipt
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&receipt)!=nil||decoder.Decode(new(any))!=io.EOF{return zero,v2input.ErrInvalid}
 encoded,err:=canonical.Marshal(receipt);if err!=nil||!bytes.Equal(encoded,raw){return zero,v2input.ErrInvalid}
 if receipt.Schema!=1||receipt.Contract!=entry.Contract||receipt.SHA!=entry.Ref.SHA256||receipt.Bytes!=size||receipt.Policy!=policy||receipt.Runtime!=runtime{return zero,v2input.ErrInvalid}
 return receipt,nil
}
