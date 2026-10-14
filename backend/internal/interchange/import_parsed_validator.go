package interchange
import (
 "context"
 "unicode/utf8"
 "github.com/google/uuid"
 "golang.org/x/text/cases"
 "photoeditor/backend/internal/v2input"
)
type importBundleEntry struct{
 ID uuid.UUID `json:"entry_id"`;External string `json:"external_id"`;ImagePath string `json:"image_path"`;ImageSHA string `json:"image_sha256"`
 RecipePath *string `json:"recipe_path"`;RecipeSHA *string `json:"recipe_sha256"`;Rating int `json:"rating"`;Pick string `json:"pick"`;Crop *importCrop `json:"crop"`;Label *string `json:"label,omitempty"`
}
type importBundleInventory struct{ID uuid.UUID `json:"artifact_id"`;Path string `json:"path"`;SHA string `json:"sha256"`;Contract string `json:"contract"`}
type importBundleMask struct{Path string `json:"path"`;SHA string `json:"sha256"`;CanvasHash string `json:"canvas_hash"`}
type importBundlePersonOrigin struct{EntryID uuid.UUID `json:"entry_id"`;Recipe v2input.Ref `json:"recipe"`;Override v2input.Ref `json:"override"`;MaskRef v2input.Ref `json:"mask_ref"`;MaskPixels v2input.Ref `json:"mask_pixels"`;Path string `json:"path"`;SHA string `json:"sha256"`}
type importParsedFile struct{Path string `json:"path"`;Content v2input.Ref `json:"content"`;Contract string `json:"contract"`}
type importBundleManifest struct{Backgrounds []bundleBackground `json:"backgrounds,omitempty"`;PersonOrigins []importBundlePersonOrigin `json:"person_origins,omitempty"`;Schema int `json:"schema_version"`;Renderer string `json:"renderer_version"`;Entries []importBundleEntry `json:"entries"`;Masks []importBundleMask `json:"masks"`;Inventory []importBundleInventory `json:"inventory"`}
type importParsedDocument struct{Schema int `json:"schema_version"`;Manifest importBundleManifest `json:"manifest"`;Files []importParsedFile `json:"files"`}
func validateParsedImportDocument(ctx context.Context,raw []byte)([]v2input.Entry,error){
 var document importParsedDocument;if err:=decodeImportDocument(raw,&document);err!=nil{return nil,err}
 manifest:=document.Manifest
 backgroundInventory:=make([]exportBundleInventoryEntry,0,len(manifest.Inventory));for _,item:=range manifest.Inventory{backgroundInventory=append(backgroundInventory,exportBundleInventoryEntry{ID:item.ID,Path:item.Path,SHA:item.SHA,Contract:item.Contract})}
 if err:=validateBundleBackgroundMappings(manifest.Backgrounds,backgroundInventory);err!=nil{return nil,err}
 if document.Schema!=1||manifest.Schema!=1||manifest.Renderer!="v2.2"||len(manifest.Entries)<1||len(manifest.Entries)>5000||manifest.Masks==nil||len(manifest.Masks)>5000||len(manifest.Inventory)<1||len(manifest.Inventory)>30000||len(document.Files)!=len(manifest.Inventory){return nil,v2input.ErrInvalid}
 inventory:=map[string]importBundleInventory{};byID:=map[uuid.UUID]importBundleInventory{};ids:=map[uuid.UUID]bool{};paths:=map[string]bool{}
 for _,item:=range manifest.Inventory{
  if err:=ctx.Err();err!=nil{return nil,err}
  folded:=cases.Fold().String(item.Path)
  if !validImportPlanPath(item.Path)||item.Path=="manifest.json"||paths[folded]||ids[item.ID]||!validImportRef(v2input.Ref{ArtifactID:item.ID,SHA256:item.SHA})||utf8.RuneCountInString(item.Contract)<1||utf8.RuneCountInString(item.Contract)>120{return nil,v2input.ErrInvalid}
  inventory[item.Path]=item;byID[item.ID]=item;paths[folded]=true;ids[item.ID]=true
 }
 children:=[]v2input.Entry{};filePaths:=map[string]bool{};fileIDs:=map[uuid.UUID]bool{}
 for _,file:=range document.Files{
  if err:=ctx.Err();err!=nil{return nil,err}
  original,ok:=inventory[file.Path]
  if !ok||filePaths[file.Path]||fileIDs[file.Content.ArtifactID]||!validImportRef(file.Content)||file.Content.SHA256!=original.SHA||file.Contract!=original.Contract{return nil,v2input.ErrInvalid}
  filePaths[file.Path]=true;fileIDs[file.Content.ArtifactID]=true
  // Archive contract labels cannot turn opaque staged bytes into trusted models.
  children=append(children,v2input.Entry{Ref:file.Content,Contract:"Artifact"})
 }
 entryIDs:=map[uuid.UUID]bool{};entriesByID:=map[uuid.UUID]importBundleEntry{};external:=map[string]bool{}
 for _,entry:=range manifest.Entries{
  if err:=ctx.Err();err!=nil{return nil,err}
  image,ok:=inventory[entry.ImagePath]
  if !ok||image.SHA!=entry.ImageSHA||entry.ID==uuid.Nil||entryIDs[entry.ID]||external[entry.External]||utf8.RuneCountInString(entry.External)<1||utf8.RuneCountInString(entry.External)>240||entry.Rating< -1||entry.Rating>5{return nil,v2input.ErrInvalid}
  entryIDs[entry.ID]=true;entriesByID[entry.ID]=entry;external[entry.External]=true
  if entry.Pick!="picked"&&entry.Pick!="rejected"&&entry.Pick!="unmarked"||!validImportColorLabel(entry.Label){return nil,v2input.ErrInvalid}
  if (entry.RecipePath==nil)!=(entry.RecipeSHA==nil){return nil,v2input.ErrInvalid}
  if entry.RecipePath!=nil{recipe,ok:=inventory[*entry.RecipePath];if !ok||recipe.SHA!=*entry.RecipeSHA{return nil,v2input.ErrInvalid}}
  if crop:=entry.Crop;crop!=nil&&(crop.X0<0||crop.Y0<0||crop.X1>1||crop.Y1>1||crop.X1<=crop.X0||crop.Y1<=crop.Y0){return nil,v2input.ErrInvalid}
 }
 masks:=map[string]bool{}
 for _,mask:=range manifest.Masks{
  original,ok:=inventory[mask.Path]
  if !ok||masks[mask.Path]||original.SHA!=mask.SHA||!validImportRef(v2input.Ref{ArtifactID:original.ID,SHA256:mask.CanvasHash}){return nil,v2input.ErrInvalid};masks[mask.Path]=true
 }
 if len(manifest.PersonOrigins)>5000{return nil,v2input.ErrInvalid}
 personKeys:=map[[3]uuid.UUID]bool{}
 for _,origin:=range manifest.PersonOrigins{
  if err:=ctx.Err();err!=nil{return nil,err}
  key:=[3]uuid.UUID{origin.EntryID,origin.Override.ArtifactID,origin.MaskRef.ArtifactID}
  entry,entryPresent:=entriesByID[origin.EntryID];entryFound:=false
  if entryPresent&&entry.RecipePath!=nil&&entry.RecipeSHA!=nil{
   recipe:=inventory[*entry.RecipePath]
   entryFound=recipe.ID==origin.Recipe.ArtifactID&&recipe.SHA==origin.Recipe.SHA256&&recipe.Contract=="EditingRecipeV3"&&*entry.RecipeSHA==origin.Recipe.SHA256
  }
  receipt,ok:=inventory[origin.Path]
  if !entryFound||personKeys[key]||!ok||receipt.Contract!="PersonOriginReceipt"||receipt.SHA!=origin.SHA||!validImportPlanPath(origin.Path){return nil,v2input.ErrInvalid}
  for _,child:=range []struct{ref v2input.Ref;contract string}{{origin.Override,"Override"},{origin.MaskRef,"MaskRef"},{origin.MaskPixels,"MaskPixels"}}{
   item,found:=byID[child.ref.ArtifactID];if !found||item.SHA!=child.ref.SHA256||item.Contract!=child.contract{return nil,v2input.ErrInvalid}
  }
  personKeys[key]=true
 }
 return children,ctx.Err()
}
