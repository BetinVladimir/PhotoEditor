package interchange
import (
 "context"
 "path"
 "sort"
 "strings"
 "unicode/utf8"
 "github.com/google/uuid"
 "golang.org/x/text/cases"
 "photoeditor/backend/internal/v2input"
)
type exportBundleEntry struct{
 ID uuid.UUID `json:"entry_id"`;External string `json:"external_id"`;ImagePath string `json:"image_path"`;ImageSHA string `json:"image_sha256"`
 RecipePath *string `json:"recipe_path"`;RecipeSHA *string `json:"recipe_sha256"`;Rating int `json:"rating"`;Pick string `json:"pick"`;Crop *importCrop `json:"crop"`;Label *string `json:"label,omitempty"`
}
type exportBundleInventoryEntry struct{ID uuid.UUID `json:"artifact_id"`;Path string `json:"path"`;SHA string `json:"sha256"`;Contract string `json:"contract"`}
type exportBundleMask struct{Path string `json:"path"`;SHA string `json:"sha256"`;Canvas string `json:"canvas_hash"`}
type exportBundleManifest struct{Backgrounds []bundleBackground `json:"backgrounds,omitempty"`;Schema int `json:"schema_version"`;Renderer string `json:"renderer_version"`;Entries []exportBundleEntry `json:"entries"`;Masks []exportBundleMask `json:"masks"`;Inventory []exportBundleInventoryEntry `json:"inventory"`}

// ValidateExportManifest binds canonical manifest bytes to a physically verified
// ZIP inventory and the frozen entries. Dependency contract labels and mask
// canvas witnesses still require semantic/native qualification before adoption.
func ValidateExportManifest(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,raw []byte,files []VerifiedExportZIPFile)error{
 if err:=ctx.Err();err!=nil{return err}
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return err}
 var plan importPlanDocument;if err:=decodeImportDocument(compiled.planBody,&plan);err!=nil{return err}
 var manifest exportBundleManifest;if err:=decodeImportDocument(raw,&manifest);err!=nil{return err}
 if err:=validateBundleBackgroundMappings(manifest.Backgrounds,manifest.Inventory);err!=nil{return err}
 if manifest.Schema!=1||manifest.Renderer!="v2.2"||manifest.Masks==nil||len(manifest.Masks)>5000||len(manifest.Entries)!=len(plan.Entries)||len(manifest.Inventory)<1||len(manifest.Inventory)>30000||len(files)>plan.Files{return v2input.ErrInvalid}
 physical:=map[string]VerifiedExportZIPFile{};folded:=map[string]bool{};total:=int64(0)
 for _,file:=range files{
  if err:=ctx.Err();err!=nil{return err}
  fold:=cases.Fold().String(file.Path)
  if !validImportPlanPath(file.Path)||folded[fold]||file.Bytes<0||file.Bytes>plan.Expanded-total||!validImportRef(v2input.Ref{ArtifactID:intent.Identity.Task,SHA256:file.SHA256}){return v2input.ErrInvalid}
  folded[fold]=true;physical[file.Path]=file;total+=file.Bytes
 }
 metadata,ok:=physical["manifest.json"];if !ok||metadata.Bytes!=int64(len(raw))||metadata.SHA256!=rawSHA(raw){return v2input.ErrInvalid};delete(physical,"manifest.json")
 inventory:=map[string]exportBundleInventoryEntry{};identities:=map[uuid.UUID]bool{};previous:=""
 for _,item:=range manifest.Inventory{
  observed,ok:=physical[item.Path]
  if !ok||!validImportRef(v2input.Ref{ArtifactID:item.ID,SHA256:item.SHA})||identities[item.ID]||item.Path<=previous||observed.SHA256!=item.SHA||!utf8.ValidString(item.Contract)||utf8.RuneCountInString(item.Contract)<1||utf8.RuneCountInString(item.Contract)>120{return v2input.ErrInvalid}
  identities[item.ID]=true;previous=item.Path;inventory[item.Path]=item
 }
 if len(inventory)!=len(physical){return v2input.ErrInvalid}
 expected:=append([]importPlanEntry(nil),plan.Entries...);sort.Slice(expected,func(i,j int)bool{return expected[i].Path<expected[j].Path})
 required:=map[string]bool{}
 for ordinal,want:=range expected{
  actual:=manifest.Entries[ordinal];image,ok:=inventory[actual.ImagePath]
  if !ok||actual.ID!=want.ID||actual.External!=want.External||actual.ImagePath!=want.Path||actual.ImageSHA!=want.Content.SHA256||image.ID!=want.Content.ArtifactID||image.SHA!=want.Content.SHA256||image.Contract!="EncodedImage"||actual.Rating!=want.Rating||actual.Pick!=want.Pick||!validImportColorLabel(actual.Label)||!sameImportColorLabel(actual.Label,want.Label)||(actual.Crop==nil)!=(want.Crop==nil){return v2input.ErrInvalid}
  if actual.Crop!=nil&&*actual.Crop!=*want.Crop{return v2input.ErrInvalid}
  required[actual.ImagePath]=true
  if plan.Format=="photoeditor_bundle"{
   if actual.RecipePath==nil||actual.RecipeSHA==nil||want.Recipe==nil{return v2input.ErrInvalid}
   recipe,ok:=inventory[*actual.RecipePath];if !ok||recipe.ID!=want.Recipe.ArtifactID||recipe.SHA!=want.Recipe.SHA256||*actual.RecipeSHA!=want.Recipe.SHA256||recipe.Contract!="EditingRecipeV3"{return v2input.ErrInvalid}
  }else{
   if actual.RecipePath!=nil||actual.RecipeSHA!=nil||len(manifest.Masks)!=0{return v2input.ErrInvalid}
   sidecarPath:=strings.TrimSuffix(want.Path,path.Ext(want.Path))+".xmp"
   sidecar,ok:=inventory[sidecarPath];if !ok||sidecar.Contract!="XMP"||sidecar.ID!=uuid.NewSHA1(want.ID,[]byte("interchange:xmp:"+sidecar.SHA)){return v2input.ErrInvalid};required[sidecarPath]=true
  }
 }
 if plan.Format!="photoeditor_bundle"&&len(required)!=len(inventory){return v2input.ErrInvalid}
 masks:=map[string]bool{}
 for _,mask:=range manifest.Masks{
  item,ok:=inventory[mask.Path]
  if !ok||masks[mask.Path]||mask.SHA!=item.SHA||(item.Contract!="MaskArray"&&item.Contract!="MaskPixels")||!validImportRef(v2input.Ref{ArtifactID:item.ID,SHA256:mask.Canvas}){return v2input.ErrInvalid};masks[mask.Path]=true
 }
 for _,item:=range manifest.Inventory{if (item.Contract=="MaskArray"||item.Contract=="MaskPixels")&&!masks[item.Path]{return v2input.ErrInvalid}}
 return ctx.Err()
}
