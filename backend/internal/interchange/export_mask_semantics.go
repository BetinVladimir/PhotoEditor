package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Model locks are independently qualified installed assets, not arbitrary locks
// found in exported documents. Every transform chain must start on a selected
// canonical source canvas; strict MaskRef parsing validates its continuity.
type ExportCanonicalMaskSemantics struct{
 canvases map[string]bool
 locks map[string]bool
 fallback ExportDependencySemantics
}
func NewExportCanonicalMaskSemantics(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,locks []string,fallback ExportDependencySemantics)(*ExportCanonicalMaskSemantics,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if len(locks)>1024||fallback==nil{return nil,v2input.ErrInvalid}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return nil,err}
 var payload exportFreezePayload;if err:=json.Unmarshal(checked.Body(),&payload);err!=nil{return nil,err}
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument([]byte(payload.SnapshotJCS),&snapshot);err!=nil{return nil,err}
 result:=&ExportCanonicalMaskSemantics{canvases:map[string]bool{},locks:map[string]bool{},fallback:fallback}
 for _,item:=range snapshot.Items{result.canvases[item.Binding.CanvasHash]=true}
 for _,lock:=range locks{if !validImportRef(v2input.Ref{ArtifactID:uuid.NameSpaceOID,SHA256:lock})||result.locks[lock]{return nil,v2input.ErrInvalid};result.locks[lock]=true}
 return result,ctx.Err()
}
func(s *ExportCanonicalMaskSemantics)QualifyExportDependency(ctx context.Context,entry v2input.Entry,raw []byte,hints []v2draft.LayerRef)error{
 if err:=ctx.Err();err!=nil{return err}
 if s==nil{return v2input.ErrInvalid}
 if entry.Contract!="MaskRef"&&entry.Contract!="ManualMask"{
  if s.fallback==nil{return v2input.ErrInvalid};return s.fallback.QualifyExportDependency(ctx,entry,append([]byte(nil),raw...),hints)
 }
 if !validImportRef(entry.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=entry.Ref.SHA256{return v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return v2input.ErrInvalid}
 if entry.Contract=="ManualMask"{
  canvas,err:=review.ParseManualVectorsCanvas(raw);if err!=nil{return err}
  digest,err:=canonical.Hash(canvas);if err!=nil{return err}
  if !s.canvases[digest]||len(hints)!=0||s.fallback==nil{return v2input.ErrInvalid}
  if err:=s.fallback.QualifyExportDependency(ctx,entry,append([]byte(nil),raw...),hints);err!=nil{return err}
  return ctx.Err()
 }
 mask,err:=review.ParseMaskRef(raw);if err!=nil{return err}
 origin:=mask.Canvas
 var chain struct{Steps []struct{From review.Canvas `json:"from_canvas"`} `json:"steps"`}
 if err:=json.Unmarshal(mask.TransformChain,&chain);err!=nil{return err}
 if len(chain.Steps)>0{origin=chain.Steps[0].From}
 digest,err:=canonical.Hash(origin);if err!=nil{return err}
 if !s.canvases[digest]{return v2input.ErrInvalid}
 if mask.ModelLockID==nil{if mask.Selector!="manual"{return v2input.ErrInvalid}}else if !s.locks[*mask.ModelLockID]{return v2input.ErrInvalid}
 // Typed hints must retain the exact mask binary edge. Physical validation is
 // performed independently by the graph's native binary resolver.
 found:=false
 for _,hint:=range hints{if hint.Ref.ArtifactID==mask.Artifact.ID{
  if hint.Ref.SHA256!=mask.Artifact.SHA256||hint.Contract!="MaskPixels"{return v2input.ErrInvalid};found=true
 }}
 if !found||s.fallback==nil{return v2input.ErrInvalid}
 if err:=s.fallback.QualifyExportDependency(ctx,entry,append([]byte(nil),raw...),hints);err!=nil{return err}
 return ctx.Err()
}
var _ ExportDependencySemantics=(*ExportCanonicalMaskSemantics)(nil)
