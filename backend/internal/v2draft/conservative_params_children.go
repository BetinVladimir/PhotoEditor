package v2draft

import (
 "bytes"
 "encoding/json"
 "strconv"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Structural projection only. The automatic operator still needs exact
// producer, protection, source and replay qualification before execution.
func conservativeParamRefs(raw []byte,path string,parent v2input.Ref)([]LayerRef,bool,error){
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return nil,false,v2input.ErrInvalid}
 var policyClaim string
 _ = json.Unmarshal(fields["policy"],&policyClaim)
 if fields["policy"]==nil||!(policyClaim=="conservative_auto_v1"||fields["image"]!=nil&&fields["resolved"]!=nil&&(fields["proposal"]!=nil||fields["proposal_report"]!=nil||fields["protected"]!=nil)){return nil,false,nil}
 invalid:=func()([]LayerRef,bool,error){return nil,true,v2input.ErrInvalid}
 if len(raw)<2||len(raw)>1<<20||len(fields)!=5{return invalid()}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(raw,normalized){return invalid()}
 var policy string
 if json.Unmarshal(fields["policy"],&policy)!=nil||policy!="conservative_auto_v1"{return invalid()}
 var image,resolved v2input.Ref
 if json.Unmarshal(fields["image"],&image)!=nil||!isDocumentRef(image)||json.Unmarshal(fields["resolved"],&resolved)!=nil||!isDocumentRef(resolved){return invalid()}
 proposalContract:="DustCandidates"
 isDefect:=fields["proposal_report"]!=nil
 required:=[]string{"policy","image","proposal","resolved","protected"}
 if isDefect{proposalContract="DefectCandidates";required=[]string{"policy","image","proposal","proposal_report","resolved"}}
 for _,key:=range required{if fields[key]==nil{return invalid()}}
 var proposal *v2input.Ref
 if json.Unmarshal(fields["proposal"],&proposal)!=nil||(proposal!=nil&&!isDocumentRef(*proposal))||(isDefect&&proposal==nil){return invalid()}
 children:=[]LayerRef{{Path:path+".image",Contract:"WorkingImage",Ref:image,ParentRef:parent}}
 if proposal!=nil{children=append(children,LayerRef{Path:path+".proposal",Contract:proposalContract,Ref:*proposal,ParentRef:parent})}
 if isDefect{
  var report v2input.Ref
  if json.Unmarshal(fields["proposal_report"],&report)!=nil||!isDocumentRef(report){return invalid()}
  children=append(children,LayerRef{Path:path+".proposal_report",Contract:"DefectProposeReport",Ref:report,ParentRef:parent})
 }else{
  var masks []json.RawMessage
  if json.Unmarshal(fields["protected"],&masks)!=nil||len(masks)<1||len(masks)>64{return invalid()}
  canvasHash:=""
  for i,rawMask:=range masks{
   mask,err:=review.ParseMaskRef(rawMask);if err!=nil||!mask.Protected{return invalid()}
   hash,err:=canonical.Hash(mask.Canvas);if err!=nil||(canvasHash!=""&&hash!=canvasHash){return invalid()};canvasHash=hash
   var refs []LayerRef
   if nestedRefs(rawMask,path+".protected["+strconv.Itoa(i)+"]","MaskRef",parent,0,&refs)!=nil{return invalid()}
   for j:=range refs{if refs[j].Contract!="MaskPixels"&&refs[j].Contract!="DenseWarpField"{return invalid()};refs[j].CanvasHash=hash}
   children=append(children,refs...)
  }
  children[0].CanvasHash=canvasHash
  if proposal!=nil{children[1].CanvasHash=canvasHash}
 }
 children=append(children,LayerRef{Path:path+".resolved",Contract:"ResolvedStyle",Ref:resolved,ParentRef:parent})
 return children,true,nil
}
