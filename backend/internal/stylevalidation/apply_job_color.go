package stylevalidation

import (
 "bytes"
 "encoding/json"
 "io"
 "math"
)

// Color mirrors GlobalColor and its residual LUT in contracts/style.py.
// Defaults are permitted only for LUT order/domain; all other fields are required.
func Color(raw json.RawMessage) bool {
 fields,ok:=applyColorObject(raw,[]string{"gains_rgb","master_curve","rgb_curves","saturation","residual_lut"},nil)
 if !ok || !applyColorNumbers(fields["gains_rgb"],3,.25,4,false) || !applyColorNumbers(fields["master_curve"],7,0,1,true) {return false}
 var curves []json.RawMessage
 if json.Unmarshal(fields["rgb_curves"],&curves)!=nil || len(curves)!=3 {return false}
 for _,curve:=range curves {if !applyColorNumbers(curve,7,0,1,true){return false}}
 if !applyColorNumber(fields["saturation"],0,2){return false}
 lut,ok:=applyColorObject(fields["residual_lut"],[]string{"kind","size","encoding","values"},[]string{"order","domain"})
 if !ok || !applyColorString(lut["kind"],"residual"){return false}
 var size int
 if json.Unmarshal(lut["size"],&size)!=nil || size!=17 {return false}
 if value,exists:=lut["order"];exists&&!applyColorString(value,"RGB_C"){return false}
 if value,exists:=lut["domain"];exists {
  var domain []json.RawMessage
  if json.Unmarshal(value,&domain)!=nil||len(domain)!=2||!applyColorNumber(domain[0],0,0)||!applyColorNumber(domain[1],1,1){return false}
 }
 count:=0
 if applyColorString(lut["encoding"],"constant"){count=3}else if applyColorString(lut["encoding"],"dense"){count=17*17*17*3}else{return false}
 return applyColorNumbers(lut["values"],count,-.25,.25,false)
}

func applyColorObject(raw json.RawMessage, required,optional []string)(map[string]json.RawMessage,bool){
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 token,err:=decoder.Token();if err!=nil||token!=json.Delim('{'){return nil,false}
 allowed:=make(map[string]bool,len(required)+len(optional));for _,key:=range required{allowed[key]=true};for _,key:=range optional{allowed[key]=true}
 fields:=make(map[string]json.RawMessage)
 for decoder.More(){token,err=decoder.Token();key,ok:=token.(string);if err!=nil||!ok||!allowed[key]{return nil,false};if _,exists:=fields[key];exists{return nil,false};var value json.RawMessage;if decoder.Decode(&value)!=nil{return nil,false};fields[key]=value}
 token,err=decoder.Token();if err!=nil||token!=json.Delim('}')||decoder.Decode(new(any))!=io.EOF{return nil,false}
 for _,key:=range required{if _,exists:=fields[key];!exists{return nil,false}}
 return fields,true
}
func applyColorString(raw json.RawMessage,want string)bool{var value *string;return json.Unmarshal(raw,&value)==nil&&value!=nil&&*value==want}
func applyColorValue(raw json.RawMessage)(float64,bool){var value *float64;if json.Unmarshal(raw,&value)!=nil||value==nil||math.IsNaN(*value)||math.IsInf(*value,0){return 0,false};return *value,true}
func applyColorNumber(raw json.RawMessage,low,high float64)bool{value,ok:=applyColorValue(raw);return ok&&value>=low&&value<=high}
func applyColorNumbers(raw json.RawMessage,count int,low,high float64,monotonic bool)bool{
 var values []json.RawMessage;if json.Unmarshal(raw,&values)!=nil||len(values)!=count{return false}
 previous:=low
 for index,rawValue:=range values{value,ok:=applyColorValue(rawValue);if !ok||value<low||value>high||monotonic&&index>0&&value<previous{return false};previous=value}
 return true
}
