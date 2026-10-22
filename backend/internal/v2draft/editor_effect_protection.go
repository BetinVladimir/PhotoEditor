package v2draft

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/editor"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// BindEditorEffectProtection carries the recipe-level protected mask into the
// same immutable Override consumed by Apply. Closure validation precedes this
// call; the exact document is independently read again before compilation SHA.
func (s ClosureResolver) BindEditorEffectProtection(ctx context.Context, who auth.Identity, tid uuid.UUID, authored []byte, binding v2input.SessionVersionBinding, sourceSHA string, compilation EditorEffectCompilation) (EditorEffectCompilation,error) {
 var recipe struct{Protected *v2input.Ref `json:"protected_mask"`}
 if json.Unmarshal(authored,&recipe)!=nil{return EditorEffectCompilation{},v2input.ErrInvalid}
 if recipe.Protected==nil{return compilation,nil}
 if s.Catalog==nil||s.Objects==nil{return EditorEffectCompilation{},v2input.ErrInvalid}
 obj,err:=s.Catalog.Lookup(ctx,who,tid,LayerRef{Path:"EditingRecipeV3.protected_mask",Contract:"MaskRef",Ref:*recipe.Protected,CanvasHash:binding.CanvasHash})
 if err!=nil{return EditorEffectCompilation{},err}
 raw,err:=readJSONArtifact(ctx,s.Objects,obj,*recipe.Protected);if err!=nil{return EditorEffectCompilation{},err}
 return bindEditorEffectProtection(compilation,raw,binding.CanvasHash,sourceSHA)
}

func bindEditorEffectProtection(compilation EditorEffectCompilation,raw []byte,canvasHash,sourceSHA string)(EditorEffectCompilation,error){
 invalid:=func()(EditorEffectCompilation,error){return EditorEffectCompilation{},v2input.ErrInvalid}
 mask,err:=review.ParseMaskRef(raw);if err!=nil||!mask.Protected||mask.Canvas.SourceSHA256!=sourceSHA{return invalid()}
 hash,err:=canonical.Hash(mask.Canvas);if err!=nil||hash!=canvasHash{return invalid()}
 var fields map[string]json.RawMessage;if json.Unmarshal(compilation.Override,&fields)!=nil{return invalid()}
 var bindings []json.RawMessage;if r,ok:=fields["mask_refs"];ok&&json.Unmarshal(r,&bindings)!=nil{return invalid()}
 // A distinct reserved binding avoids replacing any photographer-authored mask.
 const id="editor.frozen.protected"
 for _,raw:=range bindings{var item struct{ID string `json:"binding_id"`};if json.Unmarshal(raw,&item)!=nil||item.ID==id{return invalid()}}
 if len(bindings)>=128{return invalid()}
 entry,err:=canonical.Marshal(map[string]any{"binding_id":id,"purpose":"protected","mask":mask,"confirmed":true,"strength":1});if err!=nil{return invalid()}
 bindings=append(bindings,entry);fields["mask_refs"],err=canonical.Marshal(bindings);if err!=nil{return invalid()}
 compilation.Override,err=canonical.Marshal(fields);if err!=nil||editor.ValidateOverride(compilation.Override,sourceSHA)!=nil{return invalid()}
 return compilation,nil
}
