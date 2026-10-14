package v2draft

import (
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Structural dependency projection. Production scalar/safety validation and
// current producer/model/source qualification are mandatory independently.
func bodyParamRefs(raw []byte,path string,parent v2input.Ref)([]LayerRef,bool,error){
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return nil,false,v2input.ErrInvalid}
 if fields["poses"]==nil||fields["person"]==nil{return nil,false,nil}
 invalid:=func()([]LayerRef,bool,error){return nil,true,v2input.ErrInvalid}
 if len(raw)>1<<20{return invalid()}
 for key:=range fields{switch key{case "image","poses","person","fit","coefficients","faces","strength","algorithm","protect_background","max_displacement_image_fraction","on_unsafe":default:return invalid()}}
 present:=func(key string)bool{return fields[key]!=nil&&string(fields[key])!="null"}
 if present("fit")==present("coefficients"){return invalid()}
 mask,err:=review.ParseMaskRef(fields["person"]);if err!=nil||mask.Selector!="person"{return invalid()}
 canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{return invalid()}
 refs:=[]LayerRef{}
 addRef:=func(key,contract string)bool{
  var ref v2input.Ref;var keys map[string]json.RawMessage
  if json.Unmarshal(fields[key],&ref)!=nil||!isDocumentRef(ref)||json.Unmarshal(fields[key],&keys)!=nil||len(keys)!=2||keys["artifact_id"]==nil||keys["sha256"]==nil{return false}
  refs=append(refs,LayerRef{Ref:ref,Contract:contract,ParentRef:parent,Path:path+"."+key,CanvasHash:canvas});return true
 }
 if !addRef("image","WorkingImage")||!addRef("poses","PoseSet"){return invalid()}
 if err:=nestedRefs(fields["person"],path+".person","MaskRef",parent,0,&refs);err!=nil{return invalid()}
 if present("fit")&&!addRef("fit","BodyFit"){return invalid()}
 if present("faces")&&!addRef("faces","FaceLandmarks"){return invalid()}
 return refs,true,nil
}
