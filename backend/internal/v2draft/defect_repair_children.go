package v2draft

import (
 "bytes"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Structural transport only. The referring operation schema and scoped
// semantic authority must independently authorize the source/review/resolved.
func defectRepairParamRefs(raw []byte,path string,parent v2input.Ref)([]LayerRef,bool,error) {
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil {return nil,false,v2input.ErrInvalid}
 _,proposal:=fields["proposal"];_,decision:=fields["review"];_,resolved:=fields["resolved"]
 if !proposal||!decision||!resolved {return nil,false,nil}
 invalid:=func()([]LayerRef,bool,error){return nil,true,v2input.ErrInvalid}
 if len(raw)>1<<20 {return invalid()}
 var params struct {
  Image v2input.Ref `json:"image"`
  Proposal v2input.Ref `json:"proposal"`
  Review v2input.Ref `json:"review"`
  Resolved v2input.Ref `json:"resolved"`
  Protected json.RawMessage `json:"protected"`
 }
 normalized,err:=canonical.Bytes(raw);if err!=nil {return invalid()}
 decoder:=json.NewDecoder(bytes.NewReader(normalized));decoder.DisallowUnknownFields()
 if decoder.Decode(&params)!=nil {return invalid()}
 encoded,err:=canonical.Marshal(params)
 if err!=nil||!bytes.Equal(encoded,normalized)||!isDocumentRef(params.Image)||!isDocumentRef(params.Proposal)||
  !isDocumentRef(params.Review)||!isDocumentRef(params.Resolved) {return invalid()}
 mask,err:=review.ParseMaskRef(params.Protected);if err!=nil||!mask.Protected {return invalid()}
 canvas,err:=canonical.Hash(mask.Canvas);if err!=nil {return invalid()}
 children:=[]LayerRef{
  {Path:path+".image",Contract:"WorkingImage",Ref:params.Image,ParentRef:parent,CanvasHash:canvas},
  {Path:path+".proposal",Contract:"DefectCandidates",Ref:params.Proposal,ParentRef:parent,CanvasHash:canvas},
  {Path:path+".resolved",Contract:"ResolvedStyle",Ref:params.Resolved,ParentRef:parent},
  {Path:path+".review",Contract:"DefectReview",Ref:params.Review,ParentRef:parent,CanvasHash:canvas},
 }
 var masks []LayerRef
 if nestedRefs(params.Protected,path+".protected","MaskRef",parent,0,&masks)!=nil {return invalid()}
 for i:=range masks {
  if masks[i].Contract!="MaskPixels"&&masks[i].Contract!="DenseWarpField" {return invalid()}
  masks[i].CanvasHash=canvas
 }
 // Order schema properties lexically, keeping transform step array order.
 ordered:=append([]LayerRef{},children[:2]...)
 ordered=append(ordered,masks...)
 children=append(ordered,children[2:]...)
 return children,true,nil
}
