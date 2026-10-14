package v2draft

import (
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Structural projection only. Complete BodyFitParams/BodyFit native schema,
// actual paired source geometry and original producer authority remain required.
func bodyFitParamRefs(raw []byte,path string,parent v2input.Ref)([]LayerRef,bool,error){
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{return nil,false,v2input.ErrInvalid}
 if fields["before_person"]==nil&&fields["after_person"]==nil{return nil,false,nil}
 invalid:=func()([]LayerRef,bool,error){return nil,true,v2input.ErrInvalid}
 if len(raw)>1<<20{return invalid()}
 for key:=range fields{switch key{case "pair_id","before","after","alignment","before_pose","after_pose","before_person","after_person","before_faces","after_faces","algorithm","min_confidence","evidence_mode":default:return invalid()}}
 var pair uuid.UUID;if json.Unmarshal(fields["pair_id"],&pair)!=nil||pair==uuid.Nil{return invalid()}
 before,err:=review.ParseMaskRef(fields["before_person"]);if err!=nil||before.Selector!="person"{return invalid()}
 after,err:=review.ParseMaskRef(fields["after_person"]);if err!=nil||after.Selector!="person"{return invalid()}
 beforeCanvas,err:=canonical.Hash(before.Canvas);if err!=nil{return invalid()};afterCanvas,err:=canonical.Hash(after.Canvas);if err!=nil{return invalid()}
 present:=func(key string)bool{return fields[key]!=nil&&string(fields[key])!="null"}
 if present("before_faces")!=present("after_faces"){return invalid()}
 refs:=[]LayerRef{}
 add:=func(key,contract,canvas string)bool{
  var ref v2input.Ref;var keys map[string]json.RawMessage
  if json.Unmarshal(fields[key],&ref)!=nil||!isDocumentRef(ref)||json.Unmarshal(fields[key],&keys)!=nil||len(keys)!=2||keys["artifact_id"]==nil||keys["sha256"]==nil{return false}
  refs=append(refs,LayerRef{Ref:ref,Contract:contract,Path:path+"."+key,ParentRef:parent,CanvasHash:canvas});return true
 }
 for _,field:=range []struct{key,contract,canvas string}{{"before","WorkingImage",beforeCanvas},{"after","WorkingImage",afterCanvas},{"alignment","PairAlignment",""},{"before_pose","PoseSet",beforeCanvas},{"after_pose","PoseSet",afterCanvas}}{if !add(field.key,field.contract,field.canvas){return invalid()}}
 for _,mask:=range []struct{key,canvas string}{{"before_person",beforeCanvas},{"after_person",afterCanvas}}{
  start:=len(refs);if err:=nestedRefs(fields[mask.key],path+"."+mask.key,"MaskRef",parent,0,&refs);err!=nil{return invalid()};for i:=start;i<len(refs);i++{refs[i].CanvasHash=mask.canvas}
 }
 if present("before_faces"){if !add("before_faces","FaceLandmarks",beforeCanvas)||!add("after_faces","FaceLandmarks",afterCanvas){return invalid()}}
 return refs,true,nil
}
func bodyFitDocumentRefs(raw []byte,path string,parent v2input.Ref)([]LayerRef,error){
 if len(raw)<2||len(raw)>2<<20{return nil,v2input.ErrInvalid}
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{return nil,v2input.ErrInvalid}
 for key:=range fields{switch key{case "params","objects","algorithm","evidence_mode","warnings":default:return nil,v2input.ErrInvalid}}
 if fields["params"]==nil||fields["objects"]==nil{return nil,v2input.ErrInvalid}
 refs,handled,err:=bodyFitParamRefs(fields["params"],path+".params",parent);if err!=nil{return nil,err};if !handled{return nil,v2input.ErrInvalid};return refs,nil
}
