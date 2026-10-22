package interchange
import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func importedHistoricalChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 if entry.Contract=="WorkingImage"{
  image,err:=review.ParseWorkingImage(raw);if err!=nil{return nil,err}
  result:=[]v2input.Entry{{Ref:v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},Contract:"WorkingPixels"}}
  if image.Alpha!=nil{result=append(result,v2input.Entry{Ref:v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256},Contract:"WorkingAlpha"})}
  var chain struct{Steps []struct{Kind string `json:"kind"`;Field v2input.Ref `json:"inverse_field"`} `json:"steps"`};if json.Unmarshal(image.TransformChain,&chain)!=nil{return nil,v2input.ErrInvalid}
  for _,step:=range chain.Steps{if step.Kind=="dense_warp"{result=append(result,v2input.Entry{Ref:step.Field,Contract:"DenseWarpField"})}}
  return uniqueHistoricalChildren(result)
 }
 var header struct{Image v2input.Ref `json:"image"`;Faces json.RawMessage `json:"faces"`;Landmarks []struct{Transform json.RawMessage `json:"transform_chain"`} `json:"landmarks"`}
 if json.Unmarshal(raw,&header)!=nil||!validImportRef(header.Image){return nil,v2input.ErrInvalid}
 result:=[]v2input.Entry{{Ref:header.Image,Contract:"WorkingImage"}}
 if entry.Contract=="FaceLandmarks"{var boxes v2input.Ref;if json.Unmarshal(header.Faces,&boxes)!=nil||!validImportRef(boxes){return nil,v2input.ErrInvalid};result=append(result,v2input.Entry{Ref:boxes,Contract:"FaceBoxes"});for _,face:=range header.Landmarks{var chain struct{Steps []struct{Kind string `json:"kind"`;Field v2input.Ref `json:"inverse_field"`} `json:"steps"`};if json.Unmarshal(face.Transform,&chain)!=nil{return nil,v2input.ErrInvalid};for _,step:=range chain.Steps{if step.Kind=="dense_warp"{result=append(result,v2input.Entry{Ref:step.Field,Contract:"DenseWarpField"})}}}}
 if entry.Contract!="FaceLandmarks"&&entry.Contract!="FaceBoxes"{return nil,v2input.ErrInvalid}
 return uniqueHistoricalChildren(result)
}

func uniqueHistoricalChildren(children []v2input.Entry)([]v2input.Entry,error){
 result:=[]v2input.Entry{};seen:=map[[16]byte]v2input.Entry{}
 for _,child:=range children{if !validImportRef(child.Ref){return nil,v2input.ErrInvalid};if old,ok:=seen[child.Ref.ArtifactID];ok{if old!=child{return nil,v2input.ErrInvalid};continue};seen[child.Ref.ArtifactID]=child;result=append(result,child)}
 return result,nil
}
