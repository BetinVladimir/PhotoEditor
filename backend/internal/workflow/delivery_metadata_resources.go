package workflow

import (
 "bytes"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// CompileDeliveryMetadataProfile accounts for full-precision fingerprint buffers,
// compressed input copies, ExifTool rewrite and output documents. encodedBytes
// must come from current version-pinned artifact evidence, never client input.
// Estimates are conservative reservations, not measured capacity qualification.
func CompileDeliveryMetadataProfile(node CompiledDownloadNode,encodedRaw []byte,encodedBytes int64,limits DeliveryDerivativeLimits)(json.RawMessage,error){
 fail:=func()(json.RawMessage,error){return nil,domain.NewError("DELIVERY_RESOURCE_INVALID",422)}
 if node.Operation!="metadata.write"||node.Version!="1"||node.ResourceClass!="cpu"||node.TimeoutSeconds<1||len(node.Inputs)!=3||
 len(encodedRaw)<2||len(encodedRaw)>1<<20||encodedBytes<1||encodedBytes>2<<30||renderDerivativeBytesSHA(encodedRaw)!=node.Inputs[0].SHA256{return fail()}
 var image DeliveryEncodedImage
 if json.Unmarshal(encodedRaw,&image)!=nil{return fail()}
 exact,err:=canonical.Marshal(image)
 if err!=nil||!bytes.Equal(exact,encodedRaw)||image.Artifact!=node.Inputs[1]||image.Width<1||image.Height<1||
 image.Width>200000000/image.Height||image.MetadataPolicy!="strip"||domainDigestInvalid(image.ICCSHA256)||
 (image.Format!="jpeg"&&image.Format!="png"&&image.Format!="tiff")||image.Format=="jpeg"&&image.Alpha{return fail()}
 var params delivery.MetadataWriteParams
 if json.Unmarshal(node.Params,&params)!=nil{return fail()}
 paramsRaw,err:=canonical.Marshal(params)
 if err!=nil||!bytes.Equal(paramsRaw,node.Params)||params.Encoded!=(v2input.Ref{ArtifactID:node.Inputs[0].ID,SHA256:node.Inputs[0].SHA256})||
 params.Document!=(v2input.Ref{ArtifactID:node.Inputs[2].ID,SHA256:node.Inputs[2].SHA256})||params.ToolID!="exiftool_v1"||
 (params.Policy!="copyright_only"&&params.Policy!="safe_camera")||params.CopyGPS&&params.Policy!="safe_camera"{return fail()}
 pixels:=image.Width*image.Height
 // Bounded <=2GiB input and <=200M pixels keep all arithmetic below int64.
 output:=encodedBytes+(2<<20)
 estimate:=ResourceEstimate{RAMBytes:(256<<20)+pixels*64+encodedBytes*4,VRAMBytes:0,
 ScratchBytes:encodedBytes*3+(8<<20),OutputBytes:output,PixelCount:pixels}
 if limits.RAMBytes<estimate.RAMBytes||limits.ScratchBytes<estimate.ScratchBytes||limits.OutputBytes<estimate.OutputBytes{return fail()}
 return canonical.Marshal(map[string]any{"backend":"cpu","renderer_version":"v2.2","resource_estimate":estimate})
}
