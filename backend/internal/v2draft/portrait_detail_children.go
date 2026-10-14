package v2draft
import (
 "bytes"
 "encoding/json"
 "math"
 "strconv"
 "regexp"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
var portraitDetailRegionID=regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$`)

// Closed structural projection; native operation and current source/producer
// semantics must independently qualify these documents before export.
func portraitDetailParamRefs(raw []byte,path string,parent v2input.Ref)([]LayerRef,bool,error){
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{return nil,false,v2input.ErrInvalid}
 if fields["regions"]==nil||fields["resolved"]==nil||fields["base_aligned"]!=nil{return nil,false,nil}
 if len(raw)>1<<20{return nil,true,v2input.ErrInvalid}
 var params struct{Image v2input.Ref `json:"image"`;Resolved v2input.Ref `json:"resolved"`;Regions []struct{ID string `json:"region_id"`;Kind string `json:"kind"`;Mask json.RawMessage `json:"mask"`;Confirmed bool `json:"confirmed"`;Strength *float64 `json:"strength,omitempty"`} `json:"regions"`;Protected *[]json.RawMessage `json:"protected,omitempty"`}
 normalized,err:=canonical.Bytes(raw);if err!=nil{return nil,true,err}
 decoder:=json.NewDecoder(bytes.NewReader(normalized));decoder.DisallowUnknownFields();if decoder.Decode(&params)!=nil{return nil,true,v2input.ErrInvalid}
 encoded,err:=canonical.Marshal(params);if err!=nil||!bytes.Equal(encoded,normalized)||!isDocumentRef(params.Image)||!isDocumentRef(params.Resolved)||params.Regions==nil||len(params.Regions)>128||(params.Protected!=nil&&len(*params.Protected)>32){return nil,true,v2input.ErrInvalid}
 refs:=[]LayerRef{{Path:path+".image",Contract:"WorkingImage",ParentRef:parent,Ref:params.Image},{Path:path+".resolved",Contract:"ResolvedStyle",ParentRef:parent,Ref:params.Resolved}}
 add:=func(raw json.RawMessage,maskPath string,protected bool)error{
  mask,err:=review.ParseMaskRef(raw);if err!=nil{return err};if mask.Protected!=protected{return v2input.ErrInvalid}
  if !protected&&mask.Selector!="manual"&&mask.Selector!="teeth"&&mask.Selector!="sclera"&&mask.Selector!="skin"&&mask.Selector!="clothing"&&mask.Selector!="glasses"{return v2input.ErrInvalid}
  return nestedRefs(raw,maskPath,"MaskRef",parent,0,&refs)
 }
 ids:=map[string]bool{}
 for index,region:=range params.Regions{
  if !portraitDetailRegionID.MatchString(region.ID)||ids[region.ID]||(region.Strength!=nil&&(math.IsNaN(*region.Strength)||math.IsInf(*region.Strength,0)||*region.Strength<0||*region.Strength>1)){return nil,true,v2input.ErrInvalid};ids[region.ID]=true
  switch region.Kind{case "teeth","sclera","shine","under_eye":case "cloth","glasses":if !region.Confirmed{return nil,true,v2input.ErrInvalid};default:return nil,true,v2input.ErrInvalid}
  if err:=add(region.Mask,(path+".regions["+strconv.Itoa(index)+"]")+".mask",false);err!=nil{return nil,true,err}
 }
 if params.Protected!=nil{for index,mask:=range *params.Protected{if err:=add(mask,(path+".protected["+strconv.Itoa(index)+"]"),true);err!=nil{return nil,true,err}}}
 return refs,true,nil
}
