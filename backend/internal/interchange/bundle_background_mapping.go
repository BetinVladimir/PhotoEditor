package interchange
import (
 "strings"
 "unicode/utf8"
 "photoeditor/backend/internal/v2input"
)
type bundleBackground struct{
 Style v2input.Ref `json:"style"`
 Portable string `json:"portable_asset_id"`
 Path string `json:"path"`
 SHA string `json:"sha256"`
}
func validateBundleBackgroundMappings(backgrounds []bundleBackground,inventory []exportBundleInventoryEntry)error{
 if len(backgrounds)>5000{return v2input.ErrInvalid}
 byID:=map[string]exportBundleInventoryEntry{};byPath:=map[string]exportBundleInventoryEntry{}
 for _,item:=range inventory{byID[item.ID.String()]=item;byPath[item.Path]=item}
 seen:=map[string]bool{}
 for _,binding:=range backgrounds{
  key:=binding.Style.ArtifactID.String()+":"+binding.Portable
  style,styleOK:=byID[binding.Style.ArtifactID.String()];source,sourceOK:=byPath[binding.Path]
  if !validImportRef(binding.Style)||len(binding.Portable)<1||len(binding.Portable)>120||!utf8.ValidString(binding.Portable)||strings.TrimSpace(binding.Portable)!=binding.Portable||seen[key]||!validImportPlanPath(binding.Path)||!styleOK||style.Contract!="StyleV2"||style.SHA!=binding.Style.SHA256||!sourceOK||source.Contract!="StyleBackgroundAsset"||source.SHA!=binding.SHA{return v2input.ErrInvalid};seen[key]=true
 }
 return nil
}
