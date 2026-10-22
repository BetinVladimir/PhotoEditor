package v2draft

import (
 "encoding/json"
 "strconv"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Validates the immutable report structure. Independent scoped authorities
// must still prove its original defects.propose task and physical mask bytes.
func defectProposeReportRefs(raw []byte,path string,parent v2input.Ref)([]LayerRef,error){
 if len(raw)<2||len(raw)>1<<20{return nil,v2input.ErrInvalid}
 var header struct{
  Params struct{Image review.ArtifactRef `json:"image"`;Faces []map[string]json.RawMessage `json:"faces"`;Permanent json.RawMessage `json:"permanent"`;Protections []json.RawMessage `json:"protections"`} `json:"params"`
  Proposal review.ArtifactRef `json:"proposal"`
  Protection json.RawMessage `json:"protection"`
  LabelPNG review.ArtifactRef `json:"label_png"`
 }
 if json.Unmarshal(raw,&header)!=nil{return nil,v2input.ErrInvalid}
 mask,err:=review.ParseMaskRef(header.Protection);if err!=nil{return nil,err}
 source:=review.DefectSource{Image:header.Params.Image,Canvas:mask.Canvas,TransformChain:mask.TransformChain}
 if _,err:=review.ParseProposalProtection(raw,header.Proposal,source);err!=nil{return nil,err}
 canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{return nil,err}
 children:=[]LayerRef{}
 add:=func(p,contract string,ref review.ArtifactRef){children=append(children,LayerRef{Path:p,Contract:contract,Ref:v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256},ParentRef:parent,CanvasHash:canvas})}
 addMask:=func(rawMask json.RawMessage,p string)error{
  if len(rawMask)==0||string(rawMask)=="null"{return nil}
  if _,err:=review.ParseMaskRef(rawMask);err!=nil{return err}
  var refs []LayerRef
  if err:=nestedRefs(rawMask,p,"MaskRef",parent,0,&refs);err!=nil{return err}
  for i:=range refs{if refs[i].Contract!="MaskPixels"&&refs[i].Contract!="DenseWarpField"{return v2input.ErrInvalid};refs[i].CanvasHash=canvas}
  children=append(children,refs...);return nil
 }
 add(path+".label_png","DefectLabelPNG",header.LabelPNG)
 for i,face:=range header.Params.Faces{
  p:=path+".params.faces["+strconv.Itoa(i)+"]"
  var landmarks review.ArtifactRef
  if json.Unmarshal(face["landmarks"],&landmarks)!=nil{return nil,v2input.ErrInvalid}
  // Automatic repair requires the collection contract and explicit object
  // identity; a legacy single-face landmark document is not interchangeable.
  var objectID *string
  if json.Unmarshal(face["object_id"],&objectID)!=nil||objectID==nil{return nil,v2input.ErrInvalid}
  add(p+".landmarks","FaceLandmarks",landmarks)
  if err:=addMask(face["manual"],p+".manual");err!=nil{return nil,err}
  if err:=addMask(face["skin"],p+".skin");err!=nil{return nil,err}
 }
 add(path+".params.image","WorkingImage",header.Params.Image)
 if err:=addMask(header.Params.Permanent,path+".params.permanent");err!=nil{return nil,err}
 for i,protection:=range header.Params.Protections{if err:=addMask(protection,path+".params.protections["+strconv.Itoa(i)+"]");err!=nil{return nil,err}}
 add(path+".proposal","DefectCandidates",header.Proposal)
 if err:=addMask(header.Protection,path+".protection");err!=nil{return nil,err}
 return children,nil
}
