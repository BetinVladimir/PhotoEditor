package v2region

import (
 "encoding/json"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Structural occurrence projection only: worker/adopter independently validate
// ComposeParams/BlurParams and the full RelativeDepth model before native use.
func regionBackgroundChildren(raw []byte,parent v2input.Entry)([]v2draft.LayerRef,bool,error){
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return nil,false,ErrRegionInvalid}
 if fields["subject"]==nil{return nil,false,nil}
 compose:=fields["mode"]!=nil;blur:=fields["depth"]!=nil
 if compose==blur{return nil,true,ErrRegionInvalid}
 allowed:=map[string]bool{"image":true,"subject":true,"tile":true}
 if compose{for _,key:=range []string{"mode","solid_rgb","background","asset_scale","position_x","position_y","shadow_opacity","shadow_blur_fraction","shadow_offset_x","shadow_offset_y"}{allowed[key]=true}}else{allowed["depth"]=true;allowed["strength"]=true}
 for key:=range fields{if !allowed[key]{return nil,true,ErrRegionInvalid}}
 subject,err:=review.ParseMaskRef(fields["subject"])
 if err!=nil||subject.Protected||(subject.Selector!="person"&&subject.Selector!="subject"){return nil,true,ErrRegionInvalid}
 refs:=[]v2draft.LayerRef{}
 add:=func(body json.RawMessage,contract,path string)bool{
  var ref v2input.Ref;var shape map[string]json.RawMessage
  if json.Unmarshal(body,&shape)!=nil||len(shape)!=2||json.Unmarshal(body,&ref)!=nil||!regionExecutionRef(ref){return false}
  refs=append(refs,v2draft.LayerRef{Ref:ref,Contract:contract,ParentRef:parent.Ref,Path:path});return true
 }
 if !add(fields["image"],"WorkingImage","Document.image"){return nil,true,ErrRegionInvalid}
 var subjectFields map[string]json.RawMessage
 if json.Unmarshal(fields["subject"],&subjectFields)!=nil||!add(subjectFields["artifact"],"NpyArray","Document.subject.artifact"){return nil,true,ErrRegionInvalid}
 if compose{
  if value:=fields["background"];value!=nil&&string(value)!="null"{if !add(value,"WorkingImage","Document.background"){return nil,true,ErrRegionInvalid}}
 }else{
  var depth map[string]json.RawMessage
  if json.Unmarshal(fields["depth"],&depth)!=nil||len(depth)!=6{return nil,true,ErrRegionInvalid}
  for _,key:=range []string{"artifact","float32_shape","invalid_mask","canvas","transform_chain","normalization"}{if depth[key]==nil{return nil,true,ErrRegionInvalid}}
  if !add(depth["artifact"],"NpyArray","Document.depth.artifact"){return nil,true,ErrRegionInvalid}
  if string(depth["invalid_mask"])!="null"{if !add(depth["invalid_mask"],"NpyArray","Document.depth.invalid_mask"){return nil,true,ErrRegionInvalid}}
 }
 return refs,true,nil
}
