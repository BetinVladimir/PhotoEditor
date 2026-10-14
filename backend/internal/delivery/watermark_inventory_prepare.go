package delivery

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)

type WatermarkPinPreparationService interface {
 PrepareWatermarkPin(context.Context,auth.Identity,uuid.UUID,CreateIntent,uuid.UUID,string,[]byte,string,v2input.SessionAnalysisObjectReader)(FrozenWatermarkSource,error)
}
type PreparedWatermarkPin struct {Preset uuid.UUID;PresetSHA string;Pin FrozenWatermarkSource}

// PrepareWatermarkInventory validates the complete frozen preset catalogue
// before effects. Non-watermarked presets create no source pin. Earlier pins
// survive errors for exact retry, but no partial inventory escapes this method.
func PrepareWatermarkInventory(ctx context.Context,service WatermarkPinPreparationService,who auth.Identity,tid uuid.UUID,intent CreateIntent,presets []CreateCatalogPreset,bucket string,objects v2input.SessionAnalysisObjectReader)([]PreparedWatermarkPin,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if service==nil||objects==nil||tid==uuid.Nil||intent.tenant!=tid||intent.actor!=who.UserID||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||len(bucket)<3||len(bucket)>63{return nil,v2input.ErrInvalid}
 ids:=append(append([]uuid.UUID{},intent.request.Policy.RequiredPresets...),intent.request.Policy.OptionalPresets...)
 if len(ids)<1||len(ids)>16||len(presets)!=len(ids){return nil,v2input.ErrInvalid}
 owned:=make([]CreateCatalogPreset,len(presets));watermarked:=make([]bool,len(presets));seen:=map[uuid.UUID]bool{}
 for n,preset:=range presets{
  if err:=ctx.Err();err!=nil{return nil,err}
  if preset.ID!=ids[n]||preset.ID==uuid.Nil||seen[preset.ID]||len(preset.ConfigJCS)>1<<20{return nil,v2input.ErrInvalid};seen[preset.ID]=true
  config,err:=exportpreset.DecodeFrozenConfig(preset.ConfigJCS,preset.SHA256)
  if err!=nil||config.MetadataPolicy!=intent.request.Policy.MetadataPolicy{return nil,v2input.ErrInvalid}
  owned[n]=preset;owned[n].ConfigJCS=append([]byte(nil),preset.ConfigJCS...);watermarked[n]=config.Watermark!=nil
 }
 out:=make([]PreparedWatermarkPin,0,len(presets));retained:=0
 for n,preset:=range owned{
  if !watermarked[n]{continue};if err:=ctx.Err();err!=nil{return nil,err}
  pin,err:=service.PrepareWatermarkPin(ctx,who,tid,intent,preset.ID,preset.SHA256,preset.ConfigJCS,bucket,objects)
  if err!=nil{return nil,err}
  scope:=WatermarkPinScope{Tenant:tid,Project:intent.request.ProjectID,Actor:who.UserID,Parent:intent.identity.Run,Preset:preset.ID,PresetSHA:preset.SHA256}
  pin,err=DecodeWatermarkSourcePin(pin.JCS(),pin.SHA256(),scope,preset.ConfigJCS);if err!=nil{return nil,err}
  retained+=len(pin.body);if retained>32<<20{return nil,v2input.ErrInvalid}
  out=append(out,PreparedWatermarkPin{preset.ID,preset.SHA256,pin})
 }
 if err:=ctx.Err();err!=nil{return nil,err};return out,nil
}
