package interchange
import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type exportWorkingBinding struct{ref v2input.Ref;canvas string}
type ExportWorkingDocumentValidator struct{masters map[uuid.UUID]exportWorkingBinding;icc string;Additional ExportAdditionalWorkingAuthority;Dependencies ExportTypedDocumentValidator}
func NewExportWorkingDocumentValidator(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,icc string,dependencies ExportTypedDocumentValidator)(ExportWorkingDocumentValidator,error){
 if !validImportRef(v2input.Ref{ArtifactID:intent.Identity.Task,SHA256:icc}){return ExportWorkingDocumentValidator{},v2input.ErrInvalid}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return ExportWorkingDocumentValidator{},err}
 var payload exportFreezePayload;if err:=json.Unmarshal(checked.Body(),&payload);err!=nil{return ExportWorkingDocumentValidator{},err}
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument([]byte(payload.SnapshotJCS),&snapshot);err!=nil{return ExportWorkingDocumentValidator{},err}
 masters:=map[uuid.UUID]exportWorkingBinding{}
 for _,item:=range snapshot.Items{
  receipt:=exportWorkingBinding{ref:item.WorkingImage,canvas:item.Binding.CanvasHash}
  if old,exists:=masters[receipt.ref.ArtifactID];exists&&old!=receipt{return ExportWorkingDocumentValidator{},v2input.ErrInvalid}
  masters[receipt.ref.ArtifactID]=receipt
 }
 return ExportWorkingDocumentValidator{masters:masters,icc:icc,Dependencies:dependencies},nil
}
func(v ExportWorkingDocumentValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if entry.Contract!="WorkingImage"{
  if v.Dependencies==nil{return nil,v2input.ErrInvalid}
  return v.Dependencies.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...))
 }
 if !validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 expected,ok:=v.masters[entry.Ref.ArtifactID]
 if !ok{
  if v.Additional==nil{return nil,v2input.ErrInvalid}
  canvas,err:=v.Additional.QualifyAdditionalExportWorking(ctx,entry,append([]byte(nil),raw...));if err!=nil{return nil,err}
  expected=exportWorkingBinding{ref:entry.Ref,canvas:canvas}
 }
 if entry.Ref!=expected.ref{return nil,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(raw);if err!=nil{return nil,err}
 var typed review.WorkingImage;if err:=decodeImportDocument(raw,&typed);err!=nil{return nil,err}
 canvas,err:=canonical.Hash(image.Canvas)
 if err!=nil||canvas!=expected.canvas||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"||image.Color.ICCSHA256!=v.icc{return nil,v2input.ErrInvalid}
 children:=[]v2input.Entry{};seen:=map[uuid.UUID]v2input.Entry{}
 add:=func(ref v2input.Ref,contract string)error{
  child:=v2input.Entry{Ref:ref,Contract:contract}
  if old,exists:=seen[ref.ArtifactID];exists{if old!=child{return v2input.ErrInvalid};return nil}
  seen[ref.ArtifactID]=child;children=append(children,child);return nil
 }
 if err:=add(v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},"WorkingPixels");err!=nil{return nil,err}
 if image.Alpha!=nil{if err:=add(v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256},"WorkingAlpha");err!=nil{return nil,err}}
 var chain struct{Steps []struct{Inverse *v2input.Ref `json:"inverse_field"`} `json:"steps"`}
 if err:=json.Unmarshal(image.TransformChain,&chain);err!=nil{return nil,err}
 for _,step:=range chain.Steps{if step.Inverse!=nil{if err:=add(*step.Inverse,"DenseWarpField");err!=nil{return nil,err}}}
 return children,ctx.Err()
}
