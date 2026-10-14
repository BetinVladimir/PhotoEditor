package interchange
import (
 "bytes"
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Evidence source must independently authorize current project scope and prove
// the encoded image's physical/native provenance from this snapshot item.
// Frozen/portable documents must be read from exact immutable object pins.
type ExportFrameEvidence struct{Frame ExportFrame;FrozenRecipe,PortableRecipe []byte}
type ExportFrameEvidenceSource interface{
 ReadExportFrameEvidence(context.Context,v2input.SessionSnapshotItem)(ExportFrameEvidence,error)
}
type ExportLineageFrameResolver struct{items map[int]string;source ExportFrameEvidenceSource}
func NewExportLineageFrameResolver(snapshotRef v2input.Ref,raw []byte,source ExportFrameEvidenceSource)(ExportLineageFrameResolver,error){
 if source==nil{return ExportLineageFrameResolver{},v2input.ErrInvalid}
 snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(snapshotRef,raw);if err!=nil{return ExportLineageFrameResolver{},err}
 items:=map[int]string{}
 for _,item:=range snapshot.Items{body,err:=canonical.Marshal(item);if err!=nil{return ExportLineageFrameResolver{},err};items[item.Ordinal]=string(body)}
 return ExportLineageFrameResolver{items:items,source:source},nil
}
func(r ExportLineageFrameResolver)ResolveExportFrame(ctx context.Context,item v2input.SessionSnapshotItem)(ExportFrame,error){
 var zero ExportFrame
 if err:=ctx.Err();err!=nil{return zero,err}
 body,err:=canonical.Marshal(item);if err!=nil||r.source==nil||r.items[item.Ordinal]!=string(body){return zero,v2input.ErrInvalid}
 // Decode an owned item before passing its nullable pointers to the source.
 var owned v2input.SessionSnapshotItem;if err:=decodeImportDocument(body,&owned);err!=nil{return zero,err}
 evidence,err:=r.source.ReadExportFrameEvidence(ctx,owned);if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 frame:=evidence.Frame
 if !validImportRef(frame.Content)||!validImportRef(frame.PortableRecipe)||rawSHA(evidence.FrozenRecipe)!=item.Recipe.SHA256||rawSHA(evidence.PortableRecipe)!=frame.PortableRecipe.SHA256{return zero,v2input.ErrInvalid}
 for _,ref:=range []v2input.Ref{item.Binding.Source,item.WorkingImage,item.Recipe,frame.PortableRecipe}{if frame.Content.ArtifactID==ref.ArtifactID{return zero,v2input.ErrInvalid}}
 if frame.PortableRecipe.ArtifactID==item.Recipe.ArtifactID{return zero,v2input.ErrInvalid}
 frozen,err:=v2draft.DecodeFrozenRenderRecipe(evidence.FrozenRecipe);if err!=nil{return zero,err}
 if frozen.Scalars.Source!=item.Binding||frozen.RecipeSHA256!=frame.PortableRecipe.SHA256||!bytes.Equal(frozen.Recipe,evidence.PortableRecipe)||frozen.Learning!=nil{return zero,v2input.ErrInvalid}
 if err:=v2input.ValidateEditingRecipeV3(evidence.PortableRecipe,item.Binding);err!=nil{return zero,err}
 // Plan compiler validates path, rating/pick and crop bounds. Preserve exact
 // authored portable recipe bytes; never simplify edits to make export pass.
 if frame.Crop!=nil{crop:=*frame.Crop;frame.Crop=&crop}
 return frame,nil
}
var _ ExportFrameResolver=ExportLineageFrameResolver{}
