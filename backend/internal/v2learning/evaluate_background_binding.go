package v2learning

import (
 "encoding/json"
 "photoeditor/backend/internal/v2draft"
)

// Structural agreement is required in addition to current SQL authority. The
// worker repeats this against its full StyleV2 contract before decoding pixels.
func validateFrozenBackgroundBinding(styleRaw []byte,pin *v2draft.FrozenBackgroundAsset)error{
 var style struct{Extensions struct{Background *struct{
  Mode string `json:"mode"`
  AssetID *string `json:"asset_id"`
 } `json:"background"`} `json:"extensions"`}
 if json.Unmarshal(styleRaw,&style)!=nil{return ErrInvalidTrainingDraft}
 background:=style.Extensions.Background
 if background==nil{if pin!=nil{return ErrInvalidTrainingDraft};return nil}
 switch background.Mode{
 case "asset":
  if pin==nil||background.AssetID==nil||*background.AssetID!=pin.PortableAssetID{return ErrInvalidTrainingDraft}
 case "keep","solid","transparent","blur":
  if pin!=nil{return ErrInvalidTrainingDraft}
 default:return ErrInvalidTrainingDraft
 }
 return nil
}
