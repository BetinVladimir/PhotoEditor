package delivery

import (
 "bytes"
 "encoding/json"
 "math"
 "regexp"
 "sort"
 "unicode/utf8"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type MetadataGPS struct {
 Latitude float64 `json:"latitude"`
 Longitude float64 `json:"longitude"`
 Altitude *float64 `json:"altitude_m"`
}
type MetadataDocument struct {
 Author *string `json:"author"`
 Copyright *string `json:"copyright"`
 CameraMake *string `json:"camera_make"`
 CameraModel *string `json:"camera_model"`
 Lens *string `json:"lens"`
 DateTimeOriginal *string `json:"datetime_original"`
 GPS *MetadataGPS `json:"gps"`
}
type FrozenMetadata struct {
 SchemaVersion int `json:"schema_version"`
 Binding v2input.SessionVersionBinding `json:"binding"`
 MetadataSource v2input.Ref `json:"metadata_source"`
 Policy string `json:"policy"`
 ApprovedFields []string `json:"approved_fields"`
 Projection MetadataDocument `json:"projection"`
}
var metadataDatePattern=regexp.MustCompile(`^\d{4}:\d{2}:\d{2} \d{2}:\d{2}:\d{2}$`)

func(d MetadataDocument)validate()error{
 for _,value:=range []*string{d.Author,d.Copyright,d.CameraMake,d.CameraModel,d.Lens}{
  if value!=nil&&(!utf8.ValidString(*value)||utf8.RuneCountInString(*value)<1||utf8.RuneCountInString(*value)>256){return v2input.ErrInvalid}
 }
 if d.DateTimeOriginal!=nil&&!metadataDatePattern.MatchString(*d.DateTimeOriginal){return v2input.ErrInvalid}
 if d.GPS!=nil{
  g:=d.GPS
  valid:=func(v,low,high float64)bool{return !math.IsNaN(v)&&!math.IsInf(v,0)&&v>=low&&v<=high}
  if !valid(g.Latitude,-90,90)||!valid(g.Longitude,-180,180)||g.Altitude!=nil&&!valid(*g.Altitude,-1000,100000){return v2input.ErrInvalid}
 }
 return nil
}

// ProjectDeliveryMetadata owns its result. Approval/ACL must be established by
// authenticated freeze authority; a field list itself cannot grant permission.
func ProjectDeliveryMetadata(source MetadataDocument,policy string,approved []string)(MetadataDocument,error){
 if err:=source.validate();err!=nil{return MetadataDocument{},err}
 fields:=map[string]bool{"author":true,"copyright":true,"camera_make":true,"camera_model":true,"lens":true,"datetime_original":true,"gps":true}
 seen:=map[string]bool{}
 if len(approved)>7{return MetadataDocument{},v2input.ErrInvalid}
 for _,field:=range approved{if !fields[field]||seen[field]{return MetadataDocument{},v2input.ErrInvalid};seen[field]=true}
 selected:=map[string]bool{}
 switch policy{case "strip":case "copyright":selected["author"]=true;selected["copyright"]=true;case "approved_exif":selected=seen;default:return MetadataDocument{},v2input.ErrInvalid}
 raw,err:=canonical.Marshal(source);if err!=nil{return MetadataDocument{},err}
 var values map[string]json.RawMessage;if json.Unmarshal(raw,&values)!=nil{return MetadataDocument{},v2input.ErrInvalid}
 for field:=range values{if !selected[field]{values[field]=json.RawMessage("null")}}
 raw,err=canonical.Marshal(values);if err!=nil{return MetadataDocument{},err}
 var owned MetadataDocument;if json.Unmarshal(raw,&owned)!=nil{return MetadataDocument{},v2input.ErrInvalid}
 return owned,nil
}

// FreezeDeliveryMetadata verifies exact source bytes and pins the complete
// version/canvas. This compiler does not load or authorize a mutable source.
func FreezeDeliveryMetadata(binding v2input.SessionVersionBinding,ref v2input.Ref,sourceRaw []byte,policy string,approved []string)([]byte,string,error){
 invalid:=func()([]byte,string,error){return nil,"",v2input.ErrInvalid}
 if binding.AssetVersionID==uuid.Nil||binding.Source.ArtifactID==uuid.Nil||!shaPattern.MatchString(binding.Source.SHA256)||!shaPattern.MatchString(binding.CanvasHash)||ref.ArtifactID==uuid.Nil||ref.ArtifactID==binding.Source.ArtifactID||!shaPattern.MatchString(ref.SHA256)||len(sourceRaw)<2||len(sourceRaw)>1048576||digest(sourceRaw)!=ref.SHA256{return invalid()}
 var source MetadataDocument
 decoder:=json.NewDecoder(bytes.NewReader(sourceRaw));decoder.DisallowUnknownFields()
 if decoder.Decode(&source)!=nil{return invalid()}
 exact,err:=canonical.Marshal(source);if err!=nil||!bytes.Equal(exact,sourceRaw){return invalid()}
 if policy!="approved_exif"&&len(approved)!=0{return invalid()}
 projection,err:=ProjectDeliveryMetadata(source,policy,approved);if err!=nil{return invalid()}
 owned:=append([]string{},approved...);sort.Strings(owned)
 body,err:=canonical.Marshal(FrozenMetadata{SchemaVersion:1,Binding:binding,MetadataSource:ref,Policy:policy,ApprovedFields:owned,Projection:projection})
 if err!=nil{return invalid()};return body,digest(body),nil
}


// RestoreDeliveryMetadata repeats the caller's current frozen admission
// expectations. Neither policy nor approval can be selected by receipt bytes.
func RestoreDeliveryMetadata(raw []byte,pin string,binding v2input.SessionVersionBinding,ref v2input.Ref,sourceRaw []byte,policy string,approved []string)(FrozenMetadata,error){
 var zero FrozenMetadata
 if len(raw)<2||len(raw)>1048576||!shaPattern.MatchString(pin)||digest(raw)!=pin{return zero,v2input.ErrInvalid}
 expected,expectedSHA,err:=FreezeDeliveryMetadata(binding,ref,sourceRaw,policy,approved)
 if err!=nil{return zero,err}
 if expectedSHA!=pin||!bytes.Equal(expected,raw){return zero,v2input.ErrInvalid}
 var owned FrozenMetadata
 if json.Unmarshal(expected,&owned)!=nil{return zero,v2input.ErrInvalid}
 return owned,nil
}
