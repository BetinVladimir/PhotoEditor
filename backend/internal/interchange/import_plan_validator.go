package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "strings"
 "unicode/utf8"
 "github.com/google/uuid"
 "golang.org/x/text/cases"
 "golang.org/x/text/unicode/norm"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type importCrop struct{X0 float64 `json:"x0"`;Y0 float64 `json:"y0"`;X1 float64 `json:"x1"`;Y1 float64 `json:"y1"`}
type importPlanEntry struct{
 ID uuid.UUID `json:"entry_id"`;External string `json:"external_id"`;Path string `json:"path"`;Content v2input.Ref `json:"content"`
 Binding *v2input.SessionVersionBinding `json:"binding"`;Recipe *v2input.Ref `json:"recipe"`
 Rating int `json:"rating"`;Pick string `json:"pick"`;Crop *importCrop `json:"crop"`;Label *string `json:"label,omitempty"`
}
type importPlanDocument struct{
 Schema int `json:"schema_version"`;Project uuid.UUID `json:"project_id"`;Direction string `json:"direction"`;Format string `json:"format"`
 Snapshot string `json:"snapshot_digest"`;Entries []importPlanEntry `json:"entries"`
 Archive int64 `json:"max_archive_bytes"`;Expanded int64 `json:"max_expanded_bytes"`;Files int `json:"max_files"`
}
type importParamsDocument struct{Schema int `json:"schema_version"`;Parsed v2input.Ref `json:"parsed"`;Plan v2input.Ref `json:"plan"`;Evidence *v2input.Ref `json:"evidence,omitempty"`;Report int `json:"max_report_bytes"`}
func decodeImportDocument(raw []byte,target any)error{
 if len(raw)<2||len(raw)>8<<20{return v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return v2input.ErrInvalid}
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields();if err:=decoder.Decode(target);err!=nil{return err}
 encoded,err:=canonical.Marshal(target);if err!=nil||!bytes.Equal(encoded,raw){return v2input.ErrInvalid};return nil
}
func validImportRef(ref v2input.Ref)bool{
 if ref.ArtifactID==uuid.Nil||len(ref.SHA256)!=64{return false}
 for _,c:=range ref.SHA256{if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return false}};return true
}
func validImportColorLabel(label *string)bool{
 if label==nil{return true}
 switch *label{case "Red","Yellow","Green","Blue","Purple":return true}
 return false
}
func sameImportColorLabel(left,right *string)bool{
 if left==nil||right==nil{return left==nil&&right==nil}
 return *left==*right
}
func validImportPlanPath(value string)bool{
 if utf8.RuneCountInString(value)<1||utf8.RuneCountInString(value)>240||!utf8.ValidString(value)||!norm.NFC.IsNormalString(value)||strings.ContainsAny(value,"\\:\x00"){return false}
 for _,part:=range strings.Split(value,"/"){if part==""||part=="."||part==".."{return false}};return true
}
// Scope comes from authoritative current workflow evidence, not the plan body.
// Other document contracts need their own concrete typed validators.
type ImportPlanDocumentValidator struct{Project uuid.UUID;SnapshotDigest string}
func(v ImportPlanDocumentValidator)ImportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !validImportRef(entry.Ref)||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 children:=[]v2input.Entry{};seen:=map[uuid.UUID]v2input.Entry{}
 add:=func(ref v2input.Ref,contract string)error{
  if !validImportRef(ref){return v2input.ErrInvalid};child:=v2input.Entry{Ref:ref,Contract:contract}
  if old,ok:=seen[ref.ArtifactID];ok{if old!=child{return v2input.ErrInvalid};return nil}
  seen[ref.ArtifactID]=child;children=append(children,child);return nil
 }
 switch entry.Contract{
 case "ParsedInterchange":return validateParsedImportDocument(ctx,raw)
 case "Params":
  var document importParamsDocument;if err:=decodeImportDocument(raw,&document);err!=nil{return nil,err}
  if document.Schema!=1||document.Report!=8<<20||document.Parsed.ArtifactID==document.Plan.ArtifactID{return nil,v2input.ErrInvalid}
  if err:=add(document.Parsed,"ParsedInterchange");err!=nil{return nil,err};if err:=add(document.Plan,"InterchangePlan");err!=nil{return nil,err}
  if document.Evidence!=nil{if document.Evidence.ArtifactID==document.Parsed.ArtifactID||document.Evidence.ArtifactID==document.Plan.ArtifactID{return nil,v2input.ErrInvalid};if err:=add(*document.Evidence,"FrozenInterchangeImportEvidence");err!=nil{return nil,err}}
 case "InterchangePlan":
  var plan importPlanDocument;if err:=decodeImportDocument(raw,&plan);err!=nil{return nil,err}
  if v.Project==uuid.Nil||plan.Project!=v.Project||plan.Schema!=1||plan.Direction!="import"||plan.Snapshot!=v.SnapshotDigest||!validImportRef(v2input.Ref{ArtifactID:v.Project,SHA256:plan.Snapshot})||len(plan.Entries)<1||len(plan.Entries)>5000||plan.Archive<1||plan.Archive>64<<30||plan.Expanded<1||plan.Expanded>128<<30||plan.Files<1||plan.Files>30000{return nil,v2input.ErrInvalid}
  if plan.Format!="photoeditor_bundle"&&plan.Format!="xmp"&&plan.Format!="captureone_sidecar"{return nil,v2input.ErrInvalid}
  ids:=map[uuid.UUID]bool{};external:=map[string]bool{};paths:=map[string]bool{}
  for _,item:=range plan.Entries{
   if err:=ctx.Err();err!=nil{return nil,err}
   folded:=cases.Fold().String(item.Path)
   if item.ID==uuid.Nil||ids[item.ID]||utf8.RuneCountInString(item.External)<1||utf8.RuneCountInString(item.External)>240||external[item.External]||!validImportPlanPath(item.Path)||paths[folded]||item.Rating< -1||item.Rating>5{return nil,v2input.ErrInvalid}
   ids[item.ID]=true;external[item.External]=true;paths[folded]=true
   if item.Pick!="picked"&&item.Pick!="rejected"&&item.Pick!="unmarked"||!validImportColorLabel(item.Label){return nil,v2input.ErrInvalid}
   if crop:=item.Crop;crop!=nil&&(crop.X0<0||crop.Y0<0||crop.X1>1||crop.Y1>1||crop.X1<=crop.X0||crop.Y1<=crop.Y0){return nil,v2input.ErrInvalid}
   if err:=add(item.Content,"Artifact");err!=nil{return nil,err}
   if item.Binding==nil{if item.Recipe!=nil{return nil,v2input.ErrInvalid}}else{
    if item.Binding.AssetVersionID==uuid.Nil||!validImportRef(v2input.Ref{ArtifactID:item.Binding.AssetVersionID,SHA256:item.Binding.CanvasHash}){return nil,v2input.ErrInvalid}
    if err:=add(item.Binding.Source,"WorkingImage");err!=nil{return nil,err}
   }
   if item.Recipe!=nil{if err:=add(*item.Recipe,"EditingRecipeV3");err!=nil{return nil,err}}
  }
 default:return nil,v2input.ErrInvalid
 }
 return children,ctx.Err()
}
