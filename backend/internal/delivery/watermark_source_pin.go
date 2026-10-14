package delivery

import (
 "bytes"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type WatermarkPinScope struct {Tenant uuid.UUID `json:"tenant_id"`;Project uuid.UUID `json:"project_id"`;Actor uuid.UUID `json:"actor_id"`;Parent uuid.UUID `json:"parent_run_id"`;Preset uuid.UUID `json:"preset_id"`;PresetSHA string `json:"preset_sha256"`}
type watermarkPinnedArray struct {Contract string `json:"contract"`;Ref v2input.Ref `json:"ref"`;Bytes int64 `json:"bytes"`}
type watermarkPinEnvelope struct {SchemaVersion int `json:"schema_version"`;Scope WatermarkPinScope `json:"scope"`;Asset uuid.UUID `json:"asset_id"`;Binding v2input.SessionVersionBinding `json:"binding"`;Manifest string `json:"manifest_jcs"`;Arrays []watermarkPinnedArray `json:"arrays"`}
// FrozenWatermarkSource proves exact value binding, not physical NPY verification
// or SQL authority. Its bytes must be persisted before graph admission.
type FrozenWatermarkSource struct {body []byte;pin string}
func(p FrozenWatermarkSource)JCS()[]byte{return append([]byte(nil),p.body...)}
func(p FrozenWatermarkSource)SHA256()string{return p.pin}
func CompileWatermarkSourcePin(scope WatermarkPinScope,configRaw []byte,evidence WatermarkSourceEvidence)(FrozenWatermarkSource,error){
 var zero FrozenWatermarkSource
 if scope.Tenant==uuid.Nil||scope.Project==uuid.Nil||scope.Actor==uuid.Nil||scope.Parent==uuid.Nil||scope.Preset==uuid.Nil{return zero,v2input.ErrInvalid}
 config,err:=exportpreset.DecodeFrozenConfig(configRaw,scope.PresetSHA);if err!=nil||config.Watermark==nil||config.Watermark.AssetID!=evidence.Source.Asset{return zero,v2input.ErrInvalid}
 source:=evidence.Source
 if source.Version==uuid.Nil||source.Ref.ArtifactID==uuid.Nil||digest(evidence.Manifest)!=source.Ref.SHA256||evidence.Binding.AssetVersionID!=source.Version||evidence.Binding.Source!=source.Ref||len(evidence.Arrays)!=2{return zero,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(evidence.Manifest);if err!=nil||image.Alpha==nil||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"{return zero,v2input.ErrInvalid}
 if image.Canvas.Width<1||image.Canvas.Height<1||image.Canvas.Width>100000||image.Canvas.Height>100000||image.Canvas.Width*image.Canvas.Height>200000000{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(image);if err!=nil||!bytes.Equal(exact,evidence.Manifest){return zero,v2input.ErrInvalid}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil||canvas!=evidence.Binding.CanvasHash{return zero,v2input.ErrInvalid}
 expected:=[]v2input.Ref{{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256}}
 if expected[0].ArtifactID==expected[1].ArtifactID||expected[0].ArtifactID==source.Ref.ArtifactID||expected[1].ArtifactID==source.Ref.ArtifactID{return zero,v2input.ErrInvalid}
 arrays:=make([]watermarkPinnedArray,0,2)
 for n,array:=range evidence.Arrays{
  contract:="WorkingPixels";channels:=int64(3);if n==1{contract="WorkingAlpha";channels=1}
  maximum:=image.Canvas.Width*image.Canvas.Height*channels*4;minimum:=maximum;if n==1{minimum/=2}
  if array.Contract!=contract||array.Ref!=expected[n]||array.Object.SHA256!=array.Ref.SHA256||array.Object.Bytes<minimum||array.Object.Bytes>maximum+8192{return zero,v2input.ErrInvalid}
  arrays=append(arrays,watermarkPinnedArray{contract,array.Ref,array.Object.Bytes})
 }
 raw,err:=canonical.Marshal(watermarkPinEnvelope{1,scope,source.Asset,evidence.Binding,string(exact),arrays});if err!=nil||len(raw)>2<<20{return zero,v2input.ErrInvalid}
 return FrozenWatermarkSource{raw,digest(raw)},nil
}
func RestoreWatermarkSourcePin(raw []byte,pin string,scope WatermarkPinScope,configRaw []byte,evidence WatermarkSourceEvidence)(FrozenWatermarkSource,error){
 var zero FrozenWatermarkSource
 if len(raw)>2<<20||digest(raw)!=pin{return zero,v2input.ErrInvalid}
 restored,err:=CompileWatermarkSourcePin(scope,configRaw,evidence)
 if err!=nil||restored.pin!=pin||!bytes.Equal(raw,restored.body){return zero,v2input.ErrInvalid}
 return restored,nil
}

// CompileParams consumes only the owned pinned watermark manifest. Caller must
// still prove pin persistence/current slot authority and source physical arrays.
func(p FrozenWatermarkSource)CompileParams(scope WatermarkPinScope,configRaw []byte,imageRef v2input.Ref,imageRaw []byte)([]byte,string,error){
 invalid:=func()([]byte,string,error){return nil,"",v2input.ErrInvalid}
 if p.pin==""||digest(p.body)!=p.pin{return invalid()}
 var envelope watermarkPinEnvelope
 if json.Unmarshal(p.body,&envelope)!=nil||envelope.Scope!=scope{return invalid()}
 config,err:=exportpreset.DecodeFrozenConfig(configRaw,scope.PresetSHA)
 if err!=nil||config.Watermark==nil||config.Watermark.AssetID!=envelope.Asset{return invalid()}
 return CompileDeliveryWatermarkForImages(imageRef,imageRaw,envelope.Binding.Source,[]byte(envelope.Manifest),envelope.Asset,*config.Watermark)
}

// DecodeWatermarkSourcePin restores persisted value evidence against externally
// frozen request scope and config. It grants no source ACL or physical IO proof.
// Recovery must resolve this exact asset/version again, never select latest.
func DecodeWatermarkSourcePin(raw []byte,pin string,scope WatermarkPinScope,configRaw []byte)(FrozenWatermarkSource,error){
 var zero FrozenWatermarkSource
 if len(raw)<2||len(raw)>2<<20||!shaPattern.MatchString(pin)||digest(raw)!=pin{return zero,v2input.ErrInvalid}
 var envelope watermarkPinEnvelope
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&envelope)!=nil||envelope.SchemaVersion!=1||envelope.Scope!=scope{return zero,v2input.ErrInvalid}
 evidence:=WatermarkSourceEvidence{
  Source:WatermarkSource{Asset:envelope.Asset,Version:envelope.Binding.AssetVersionID,Ref:envelope.Binding.Source},
  Binding:envelope.Binding,Manifest:[]byte(envelope.Manifest),
 }
 for _,array:=range envelope.Arrays{evidence.Arrays=append(evidence.Arrays,WatermarkArrayObject{
  Contract:array.Contract,Ref:array.Ref,Object:objectstore.Object{SHA256:array.Ref.SHA256,Bytes:array.Bytes},
 })}
 // Exact recompile also rejects trailing JSON, duplicate keys, and noncanonical
 // representations without trusting the decoder's interpretation of them.
 return RestoreWatermarkSourcePin(raw,pin,scope,configRaw,evidence)
}
