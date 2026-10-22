package exportjob

import (
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/exportpreset"
)

// PresetPin is read under the selected project's tenant transaction. Config is
// validated with its exact canonical hash before any Delivery request is made.
type PresetPin struct { ID uuid.UUID; SHA string; Config exportpreset.Config }
type DeliveryGroup struct { Key string `json:"key"`; Request delivery.CreateRequest `json:"request"` }

// compileDeliveryGroups keeps metadata consent explicit and never strips a
// preset's presentation controls. Each group is a normal fully qualified
// Delivery, with immutable expected preset pins and its own stable retry key.
func compileDeliveryGroups(tid uuid.UUID, who auth.Identity, batch, project, snapshot uuid.UUID, pins []PresetPin, approved []string) ([]DeliveryGroup,error) {
 invalid:=func()([]DeliveryGroup,error){return nil,domain.NewError("VALIDATION_FAILED",422)}
 if batch==uuid.Nil || project==uuid.Nil || snapshot==uuid.Nil || len(pins)<1 || len(pins)>16{return invalid()}
 groups:=[]DeliveryGroup{}; byPolicy:=map[string]int{};seen:=map[uuid.UUID]bool{}
 for _,pin:=range pins {
  if pin.ID==uuid.Nil||seen[pin.ID]||exportpreset.ValidateConfig(pin.Config)!=nil{return invalid()};seen[pin.ID]=true
  hash,e:=canonical.Hash(pin.Config);if e!=nil||hash!=pin.SHA{return invalid()}
  policy:=pin.Config.MetadataPolicy;index,exists:=byPolicy[policy]
  if !exists {
   index=len(groups);byPolicy[policy]=index
   key:=uuid.NewSHA1(batch,[]byte("export:delivery:"+policy)).String()
   fields:=[]string(nil);if policy=="approved_exif"{fields=append([]string(nil),approved...)}
   groups=append(groups,DeliveryGroup{Key:key,Request:delivery.CreateRequest{ProjectID:project,SnapshotID:snapshot,Name:"Export · "+policy,Policy:delivery.Policy{RequiredPresets:[]uuid.UUID{},OptionalPresets:[]uuid.UUID{},RequireReview:true,MetadataPolicy:policy,ApprovedFields:fields,Provenance:"off",AllowOmitted:false},ExpectedPresetSHA:map[uuid.UUID]string{}}})
  }
  groups[index].Request.Policy.RequiredPresets=append(groups[index].Request.Policy.RequiredPresets,pin.ID)
  groups[index].Request.ExpectedPresetSHA[pin.ID]=pin.SHA
 }
 if len(approved)>0 {if _,ok:=byPolicy["approved_exif"];!ok{return invalid()}}
 for _,group:=range groups{if _,err:=delivery.FreezeCreateIntent(tid,who,group.Request,group.Key);err!=nil{return nil,err}}
 return groups,nil
}
