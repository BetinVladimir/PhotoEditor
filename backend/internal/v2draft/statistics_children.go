package v2draft
import (
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Source/producer qualification must follow structural extraction.
func statsParamRefs(raw []byte,path string,parent v2input.Ref)([]LayerRef,bool,error){
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return nil,false,v2input.ErrInvalid}
 // BodyParams also has an inline person mask; image.local owns its own
 // aligned-base semantics. Neither may be parsed as color.stats.
 if fields["poses"]!=nil||fields["base_aligned"]!=nil{return nil,false,nil}
 if _,present:=fields["person"];!present{return nil,false,nil}
 params,err:=review.ParseStatsParams(raw);if err!=nil{return nil,true,v2input.ErrInvalid}
 canvas,err:=canonical.Hash(params.Person.Canvas);if err!=nil{return nil,true,err}
 children:=[]LayerRef{{Path:path+".image",Contract:"WorkingImage",ParentRef:parent,CanvasHash:canvas,
  Ref:v2input.Ref{ArtifactID:params.Image.ID,SHA256:params.Image.SHA256}}}
 var masks []LayerRef
 if err:=nestedRefs(fields["person"],path+".person","MaskRef",parent,0,&masks);err!=nil{return nil,true,err}
 for i:=range masks {
  if masks[i].Contract!="MaskPixels"&&masks[i].Contract!="DenseWarpField"{return nil,true,v2input.ErrInvalid}
  masks[i].CanvasHash=canvas
 }
 return append(children,masks...),true,nil
}
