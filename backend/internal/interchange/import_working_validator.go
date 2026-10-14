package interchange
import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type importWorkingEvidence struct{ref v2input.Ref;canvas,original string}
type ImportScopedDocumentValidator struct{roots ImportPlanDocumentValidator;masters map[uuid.UUID]importWorkingEvidence;witnesses *ImportWorkingWitnesses;planRaw,parsedRaw []byte}
// Build from owned, typed Plan/Parsed bytes. SQL catalog must independently prove
// that each master is the current canonical ingest source for this workflow.
func NewImportScopedDocumentValidator(project uuid.UUID,snapshot string,planRaw,parsedRaw []byte)(ImportScopedDocumentValidator,error){
 roots:=ImportPlanDocumentValidator{Project:project,SnapshotDigest:snapshot}
 if _,err:=roots.ImportDocumentChildren(context.Background(),v2input.Entry{Ref:v2input.Ref{ArtifactID:project,SHA256:rawSHA(planRaw)},Contract:"InterchangePlan"},planRaw);err!=nil{return ImportScopedDocumentValidator{},err}
 if _,err:=validateParsedImportDocument(context.Background(),parsedRaw);err!=nil{return ImportScopedDocumentValidator{},err}
 var plan importPlanDocument;var parsed importParsedDocument
 if err:=decodeImportDocument(planRaw,&plan);err!=nil{return ImportScopedDocumentValidator{},err};if err:=decodeImportDocument(parsedRaw,&parsed);err!=nil{return ImportScopedDocumentValidator{},err}
 if len(plan.Entries)!=len(parsed.Manifest.Entries){return ImportScopedDocumentValidator{},v2input.ErrInvalid}
 files:=map[string]v2input.Ref{};for _,file:=range parsed.Files{files[file.Path]=file.Content}
 masters:=map[uuid.UUID]importWorkingEvidence{}
 for ordinal,entry:=range plan.Entries{
  original:=parsed.Manifest.Entries[ordinal]
  if entry.ID!=original.ID||entry.External!=original.External||entry.Path!=original.ImagePath||entry.Content!=files[original.ImagePath]||entry.Rating!=original.Rating||entry.Pick!=original.Pick||!sameImportCrop(entry.Crop,original.Crop){return ImportScopedDocumentValidator{},v2input.ErrInvalid}
  if entry.Binding!=nil{
   evidence:=importWorkingEvidence{ref:entry.Binding.Source,canvas:entry.Binding.CanvasHash,original:original.ImageSHA}
   if old,ok:=masters[evidence.ref.ArtifactID];ok&&old!=evidence{return ImportScopedDocumentValidator{},v2input.ErrInvalid};masters[evidence.ref.ArtifactID]=evidence
  }
 }
 return ImportScopedDocumentValidator{roots:roots,masters:masters,witnesses:&ImportWorkingWitnesses{},planRaw:append([]byte(nil),planRaw...),parsedRaw:append([]byte(nil),parsedRaw...)},nil
}
func sameImportCrop(left,right *importCrop)bool{
 if left==nil||right==nil{return left==nil&&right==nil}
 return *left==*right
}
func(v ImportScopedDocumentValidator)ImportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if entry.Contract=="FrozenInterchangeImportEvidence"{return v.importEvidenceChildren(ctx,entry,raw)}
 if entry.Contract!="WorkingImage"{return v.roots.ImportDocumentChildren(ctx,entry,raw)}
 if err:=ctx.Err();err!=nil{return nil,err}
 expected,ok:=v.masters[entry.Ref.ArtifactID]
 if !ok||entry.Ref!=expected.ref||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(raw);if err!=nil{return nil,err}
 // Require the existing typed roundtrip to retain every required field.
 var typed review.WorkingImage;if err:=decodeImportDocument(raw,&typed);err!=nil{return nil,err}
 canvas,err:=canonical.Hash(image.Canvas)
 if err!=nil||canvas!=expected.canvas||image.Canvas.SourceSHA256!=expected.original||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"{return nil,v2input.ErrInvalid}
 refs,err:=importDocumentRefs(raw,"WorkingImage");if err!=nil{return nil,err}
 roles:=map[uuid.UUID]v2input.Entry{}
 addRole:=func(ref review.ArtifactRef,contract string)error{
  child:=v2input.Entry{Ref:v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256},Contract:contract}
  if !validImportRef(child.Ref){return v2input.ErrInvalid}
  if previous,ok:=roles[ref.ID];ok&&previous!=child{return v2input.ErrInvalid}
  roles[ref.ID]=child;return nil
 }
 if err:=addRole(image.Pixels,"WorkingPixels");err!=nil{return nil,err}
 if image.Alpha!=nil{if err:=addRole(*image.Alpha,"WorkingAlpha");err!=nil{return nil,err}}
 var chain struct{Steps []struct{Inverse *review.ArtifactRef `json:"inverse_field"`} `json:"steps"`}
 if json.Unmarshal(image.TransformChain,&chain)!=nil{return nil,v2input.ErrInvalid}
 for _,step:=range chain.Steps{if step.Inverse!=nil{if err:=addRole(*step.Inverse,"DenseWarpField");err!=nil{return nil,err}}}
 children:=[]v2input.Entry{};seen:=map[uuid.UUID]bool{}
 for _,ref:=range refs{
  child,ok:=roles[ref.ArtifactID];if !ok||child.Ref!=ref{return nil,v2input.ErrInvalid}
  if seen[ref.ArtifactID]{continue};seen[ref.ArtifactID]=true
  children=append(children,child)
 }
 if len(children)!=len(roles){return nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return nil,err}
 if err:=v.witnesses.remember(entry.Ref,raw,children);err!=nil{return nil,err}
 return children,nil
}
