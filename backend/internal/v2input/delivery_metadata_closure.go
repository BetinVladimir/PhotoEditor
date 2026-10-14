package v2input

import (
 "bytes"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

// CompileDeliveryMetadataClosure owns exactly three verified metadata inputs
// plus Params. Physical bytes and frozen source approval are caller obligations.
func CompileDeliveryMetadataClosure(workflow uuid.UUID,paramsRaw []byte,paramsRef,encoded,raster,projection Ref)(CaptureClosure,error){
 task,err:=TaskID(workflow,"metadata.write",0)
 if err!=nil||len(paramsRaw)<2||len(paramsRaw)>65536{return CaptureClosure{},ErrInvalid}
 var params struct {
  Encoded Ref `json:"encoded"`
  Document Ref `json:"document"`
  Policy string `json:"policy"`
  CopyGPS bool `json:"copy_gps"`
  ToolID string `json:"tool_id"`
 }
 if json.Unmarshal(paramsRaw,&params)!=nil{return CaptureClosure{},ErrInvalid}
 exact,err:=canonical.Marshal(params)
 if err!=nil||!bytes.Equal(exact,paramsRaw)||params.Encoded!=encoded||params.Document!=projection||params.ToolID!="exiftool_v1"||
 (params.Policy!="copyright_only"&&params.Policy!="safe_camera")||params.CopyGPS&&params.Policy!="safe_camera"{return CaptureClosure{},ErrInvalid}
 refs:=[]Ref{paramsRef,encoded,raster,projection};seen:=map[uuid.UUID]bool{}
 for _,ref:=range refs{if !validRef(ref)||seen[ref.ArtifactID]{return CaptureClosure{},ErrInvalid};seen[ref.ArtifactID]=true}
 if paramsRef.ArtifactID!=uuid.NewSHA1(task,[]byte("delivery:metadata:params:1"))||
 projection.ArtifactID!=uuid.NewSHA1(task,[]byte("delivery:metadata:projection:1")){return CaptureClosure{},ErrInvalid}
 return compileCaptureInventory(task,"metadata.write",json.RawMessage(exact),paramsRef,[]Entry{
 {paramsRef,"Params"},{encoded,"EncodedImage"},{raster,"EncodedRaster"},{projection,"MetadataDocument"}})
}
