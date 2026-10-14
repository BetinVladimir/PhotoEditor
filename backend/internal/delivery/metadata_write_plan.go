package delivery

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type metadataEncodedImage struct {
 Artifact v2input.Ref `json:"artifact"`
 Format string `json:"format"`
 Width int64 `json:"width"`
 Height int64 `json:"height"`
 ICCID string `json:"icc_id"`
 ICCSHA string `json:"icc_sha256"`
 Alpha bool `json:"alpha"`
 Policy string `json:"metadata_policy"`
}
type MetadataWriteParams struct {
 Encoded v2input.Ref `json:"encoded"`
 Document v2input.Ref `json:"document"`
 Policy string `json:"policy"`
 CopyGPS bool `json:"copy_gps"`
 ToolID string `json:"tool_id"`
}
type MetadataWritePlan struct {
 params,projection v2input.Ref
 inputs []v2input.Ref
 paramsJCS,projectionJCS []byte
}
func(p MetadataWritePlan)Params()v2input.Ref{return p.params}
func(p MetadataWritePlan)Projection()v2input.Ref{return p.projection}
func(p MetadataWritePlan)Inputs()[]v2input.Ref{return append([]v2input.Ref(nil),p.inputs...)}
func(p MetadataWritePlan)ParamsJCS()[]byte{return append([]byte(nil),p.paramsJCS...)}
func(p MetadataWritePlan)ProjectionJCS()[]byte{return append([]byte(nil),p.projectionJCS...)}

// CompileMetadataWrite uses the existing metadata.write/1 worker contract. It
// grants no admission/publication authority; the coordinator must retain the
// frozen receipt pin and recheck it when completing the child task.
func CompileMetadataWrite(task uuid.UUID,encoded v2input.Ref,encodedRaw,raw []byte,pin string,
 binding v2input.SessionVersionBinding,metadataRef v2input.Ref,sourceRaw []byte,policy string,approved []string)(MetadataWritePlan,error){
 invalid:=func()(MetadataWritePlan,error){return MetadataWritePlan{},v2input.ErrInvalid}
 validRef:=func(r v2input.Ref)bool{return r.ArtifactID!=uuid.Nil&&shaPattern.MatchString(r.SHA256)}
 if task==uuid.Nil||!validRef(encoded)||len(encodedRaw)<2||len(encodedRaw)>1048576||digest(encodedRaw)!=encoded.SHA256||
 (policy!="copyright"&&policy!="approved_exif"){return invalid()}
 frozen,err:=RestoreDeliveryMetadata(raw,pin,binding,metadataRef,sourceRaw,policy,approved)
 if err!=nil{return MetadataWritePlan{},err}
 var image metadataEncodedImage
 decoder:=json.NewDecoder(bytes.NewReader(encodedRaw));decoder.DisallowUnknownFields()
 if decoder.Decode(&image)!=nil{return invalid()}
 canonicalImage,err:=canonical.Marshal(image)
 if err!=nil||!bytes.Equal(canonicalImage,encodedRaw)||!validRef(image.Artifact)||image.Policy!="strip"||
 (image.Format!="jpeg"&&image.Format!="png"&&image.Format!="tiff")||image.Width<1||image.Height<1||image.Width>200000000/image.Height||
 (image.ICCID!="srgb_v1"&&image.ICCID!="work_rec2020_gamma24_v1")||!shaPattern.MatchString(image.ICCSHA)||image.Format=="jpeg"&&image.Alpha{return invalid()}
 projection,err:=canonical.Marshal(frozen.Projection)
 if err!=nil{return invalid()}
 projectionRef:=v2input.Ref{ArtifactID:uuid.NewSHA1(task,[]byte("delivery:metadata:projection:1")),SHA256:digest(projection)}
 params:=MetadataWriteParams{Encoded:encoded,Document:projectionRef,Policy:"copyright_only",ToolID:"exiftool_v1"}
 if policy=="approved_exif"{params.Policy="safe_camera";for _,field:=range frozen.ApprovedFields{if field=="gps"{params.CopyGPS=true}}}
 paramsRaw,err:=canonical.Marshal(params)
 if err!=nil{return invalid()}
 paramsRef:=v2input.Ref{ArtifactID:uuid.NewSHA1(task,[]byte("delivery:metadata:params:1")),SHA256:digest(paramsRaw)}
 seen:=map[uuid.UUID]bool{}
 for _,ref:=range []v2input.Ref{encoded,image.Artifact,metadataRef,binding.Source,projectionRef,paramsRef}{
  if seen[ref.ArtifactID]{return invalid()};seen[ref.ArtifactID]=true
 }
 return MetadataWritePlan{params:paramsRef,projection:projectionRef,inputs:[]v2input.Ref{encoded,image.Artifact,projectionRef},paramsJCS:paramsRaw,projectionJCS:projection},nil
}


// InputClosure binds the exact compiled writer inputs; it performs no object IO.
func(p MetadataWritePlan)InputClosure(childRun uuid.UUID)(v2input.CaptureClosure,error){
 workflow,err:=v2input.WorkflowID(childRun)
 if err!=nil||len(p.inputs)!=3{return v2input.CaptureClosure{},v2input.ErrInvalid}
 return v2input.CompileDeliveryMetadataClosure(workflow,p.ParamsJCS(),p.params,p.inputs[0],p.inputs[1],p.inputs[2])
}

// PublishInputs writes only transport documents after the blocked shell commits.
func(p MetadataWritePlan)PublishInputs(ctx context.Context,childRun uuid.UUID,publisher v2input.Publisher)(v2input.GraphTask,error){
 if err:=ctx.Err();err!=nil{return v2input.GraphTask{},err}
 if publisher==nil{return v2input.GraphTask{},v2input.ErrInvalid}
 closure,err:=p.InputClosure(childRun)
 if err!=nil{return v2input.GraphTask{},err}
 workflow,_:=v2input.WorkflowID(childRun);task,_:=v2input.TaskID(workflow,"metadata.write",0)
 cursor,err:=closure.Open(ctx)
 if err!=nil{return v2input.GraphTask{},err}
 built,err:=v2input.BuildInputClosure(ctx,task,cursor,publisher)
 if err!=nil{return v2input.GraphTask{},err}
 return v2input.GraphTask{ID:task,Built:built,Open:closure.Open,Direct:append([]v2input.Ref(nil),closure.Direct...)},nil
}
