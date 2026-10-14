package interchange
import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Reuses the native array protocol, but derives geometry exclusively from the
// canonical import witness and repeats the dedicated current SQL catalogue.
type ImportCanonicalWarpGeometry struct{Witnesses *ImportWorkingWitnesses;Catalog ImportDocumentCatalog}
func(s ImportCanonicalWarpGeometry)ResolveExportArrayGeometry(ctx context.Context,entry v2input.Entry)(ExportArrayGeometry,error){
 var zero ExportArrayGeometry
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Catalog==nil||entry.Contract!="DenseWarpField"||!validImportRef(entry.Ref){return zero,v2input.ErrInvalid}
 witness,ok:=s.Witnesses.lookup(entry);if !ok||rawSHA(witness.body)!=witness.master.SHA256{return zero,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(witness.body);if err!=nil{return zero,err}
 var chain struct{Steps []struct{Kind string `json:"kind"`;To review.Canvas `json:"to_canvas"`;Inverse *review.ArtifactRef `json:"inverse_field"`} `json:"steps"`}
 if json.Unmarshal(image.TransformChain,&chain)!=nil{return zero,v2input.ErrInvalid}
 found:=false;var result ExportArrayGeometry
 for _,step:=range chain.Steps{if step.Inverse!=nil&&step.Inverse.ID==entry.Ref.ArtifactID{
  if step.Kind!="dense_warp"||step.Inverse.SHA256!=entry.Ref.SHA256{return zero,v2input.ErrInvalid}
  canvas,err:=canonical.Hash(step.To);if err!=nil{return zero,err}
  geometry:=ExportArrayGeometry{Width:int(step.To.Width),Height:int(step.To.Height),CanvasSHA:canvas,DType:"float32"}
  if _,err:=exportArrayMaximumBytes(entry,geometry);err!=nil{return zero,err}
  if found&&result!=geometry{return zero,v2input.ErrInvalid};result=geometry;found=true
 }}
 if !found{return zero,v2input.ErrInvalid}
 if _,err:=s.Catalog.LookupImportDocument(ctx,entry);err!=nil{return zero,err}
 return result,ctx.Err()
}
