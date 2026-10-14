package v2draft

import (
 "encoding/json"
 "strconv"
 "unicode/utf8"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Structural projection only; production schema, current source geometry and
// operation authority are separately mandatory at export admission.
func imageLocalParamRefs(raw []byte,path string,parent v2input.Ref)([]LayerRef,bool,error){
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return nil,false,v2input.ErrInvalid}
 if fields["base_aligned"]==nil{return nil,false,nil}
 invalid:=func()([]LayerRef,bool,error){return nil,true,v2input.ErrInvalid}
 if len(raw)>1<<20{return invalid()}
 for key:=range fields{switch key{case "image","base_aligned","resolved","series","person","regions","item_id":default:return invalid()}}
 refs:=[]LayerRef{}
 for _,field:=range []struct{key,contract string;optional bool}{{"image","WorkingImage",false},{"base_aligned","WorkingImage",false},{"resolved","ResolvedStyle",false},{"series","SeriesCorrections",true}}{
  body:=fields[field.key];if field.optional&&(body==nil||string(body)=="null"){continue}
  var ref v2input.Ref;if json.Unmarshal(body,&ref)!=nil||!isDocumentRef(ref){return invalid()}
  var keys map[string]json.RawMessage;if json.Unmarshal(body,&keys)!=nil||len(keys)!=2||keys["artifact_id"]==nil||keys["sha256"]==nil{return invalid()}
  refs=append(refs,LayerRef{Path:path+"."+field.key,Contract:field.contract,Ref:ref,ParentRef:parent})
 }
 if item:=fields["item_id"];item!=nil&&string(item)!="null"{var id uuid.UUID;if json.Unmarshal(item,&id)!=nil{return invalid()}}
 add:=func(body json.RawMessage,p string,person bool)error{mask,err:=review.ParseMaskRef(body);if err!=nil{return err};if person&&mask.Selector!="person"{return v2input.ErrInvalid};return nestedRefs(body,p,"MaskRef",parent,0,&refs)}
 if person:=fields["person"];person!=nil&&string(person)!="null"{if add(person,path+".person",true)!=nil{return invalid()}}
 if regions:=fields["regions"];regions!=nil{
  var items []map[string]json.RawMessage;if json.Unmarshal(regions,&items)!=nil||items==nil||len(items)>128{return invalid()}
  ids:=map[string]bool{}
  for index,item:=range items{
   var id string;if len(item)!=2||json.Unmarshal(item["region_id"],&id)!=nil||utf8.RuneCountInString(id)<1||utf8.RuneCountInString(id)>120||ids[id]||item["mask"]==nil{return invalid()};ids[id]=true
   if add(item["mask"],path+".regions["+strconv.Itoa(index)+"].mask",false)!=nil{return invalid()}
  }
 }
 return refs,true,nil
}
