package v2region

import (
 "bytes"
 "context"
 "encoding/json"
 "sort"
 "photoeditor/backend/internal/review"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Execution closure has per-stage provenance; it must never use the generic
// editor resolver's single original-canvas binding for intermediate images.
// Contract-specific scalar validation is repeated by the worker before replay.
func(c regionExecutionCatalog) executionInputs(ctx context.Context,candidate regionExecutionCandidate,execution RegionExecutionDescriptor,base []v2input.Entry)([]v2input.Entry,error){
 inventory:=map[uuid.UUID]v2input.Entry{}
 for _,entry:=range base{inventory[entry.Ref.ArtifactID]=entry}
 type occurrence struct{entry v2input.Entry;task uuid.UUID}
 queue:=[]occurrence{}
 add:=func(ref v2input.Ref,contract string,task uuid.UUID){queue=append(queue,occurrence{v2input.Entry{Ref:ref,Contract:contract},task})}
 for _,stage:=range execution.Stages{
  add(stage.Params,"Params",stage.TaskID);add(stage.Source,"WorkingImage",stage.TaskID);add(stage.Output,"WorkingImage",stage.TaskID)
  if stage.Plan!=nil{add(*stage.Plan,"CompositionPlan",stage.TaskID)}
  if stage.PolicyParams!=nil{add(*stage.PolicyParams,"Params",stage.TaskID)}
  if stage.Report!=nil{add(*stage.Report,"BodyWarpReport",stage.TaskID)}
  if len(stage.ReviewAuthority)>0{
   var authority struct{Route string `json:"route"`;Selection struct{Image v2input.Ref `json:"image"`;Proposal v2input.Ref `json:"proposal"`;LabelMap v2input.Ref `json:"label_map"`} `json:"selection"`}
   if json.Unmarshal(stage.ReviewAuthority,&authority)!=nil{return nil,ErrRegionInvalid}
   if authority.Route=="base"{add(authority.Selection.Image,"WorkingImage",stage.TaskID);add(authority.Selection.Proposal,"DefectCandidates",stage.TaskID);add(authority.Selection.LabelMap,"NpyArray",stage.TaskID)}
  }
  if stage.BackgroundAsset!=nil{add(v2input.Ref{ArtifactID:stage.BackgroundAsset.Source.ID,SHA256:stage.BackgroundAsset.Source.SHA256},"EncodedImage",stage.TaskID);add(*stage.BackgroundParams,"Params",stage.TaskID);add(*stage.BackgroundImage,"WorkingImage",stage.TaskID)}
 }
 visited:=map[string]bool{}
 for cursor:=0;cursor<len(queue);cursor++{
  if err:=ctx.Err();err!=nil{return nil,err}
  if len(queue)>4096||len(inventory)>511{return nil,ErrRegionInvalid}
  node:=queue[cursor];key:=node.task.String()+":"+node.entry.Ref.ArtifactID.String()+":"+node.entry.Contract
  if visited[key]{continue};visited[key]=true
  if !regionExecutionRef(node.entry.Ref){return nil,ErrRegionInvalid}
  if prior,ok:=inventory[node.entry.Ref.ArtifactID];ok&&prior!=node.entry{return nil,ErrRegionInvalid}
  object,_,err:=c.document(ctx,candidate.RunID,node.task,node.entry.Ref);if err!=nil{return nil,err}
  inventory[node.entry.Ref.ArtifactID]=node.entry
  if node.entry.Contract=="EncodedImage"{
   if object.Bytes>536870912||(object.ContentType!="image/jpeg"&&object.ContentType!="image/png"&&object.ContentType!="image/tiff"){return nil,ErrRegionInvalid};continue
  }
  if node.entry.Contract=="DefectLabelPNG"{if object.Bytes>536870912||object.ContentType!="image/png"{return nil,ErrRegionInvalid};continue}
  if node.entry.Contract=="NpyArray"{
   if object.ContentType!="application/octet-stream"&&object.ContentType!="application/x-npy"{return nil,ErrRegionInvalid};continue
  }
  if object.ContentType!="application/json"{return nil,ErrRegionInvalid}
  raw,err:=readRegionDocument(ctx,c.Objects,object,node.entry.Ref);if err!=nil{return nil,err}
  children,err:=regionExecutionChildren(raw,node.entry);if err!=nil{return nil,err}
  for _,child:=range children{add(child.Ref,child.Contract,node.task)}
 }
 if len(inventory)>511{return nil,ErrRegionInvalid}
 result:=make([]v2input.Entry,0,len(inventory));for _,entry:=range inventory{result=append(result,entry)}
 sort.Slice(result,func(i,j int)bool{return bytes.Compare(result[i].Ref.ArtifactID[:],result[j].Ref.ArtifactID[:])<0})
 return result,nil
}

func regionExecutionChildren(raw []byte,parent v2input.Entry)([]v2draft.LayerRef,error){
 if parent.Contract=="Params"{if refs,recognized,err:=regionBackgroundChildren(raw,parent);recognized||err!=nil{return refs,err}}
 var children []v2draft.LayerRef;var err error
 fields:=map[string]string{}
 switch parent.Contract{
 case "CompositionPlan":fields=map[string]string{"image":"WorkingImage","structure":"ImageStructure"}
 case "ImageStructure","FaceBoxes","PoseSet":fields=map[string]string{"image":"WorkingImage"}
 case "FaceLandmarks":fields=map[string]string{"image":"WorkingImage","faces":"FaceBoxes"}
 case "StyleV2":
  if _,err:=v2draft.DecodeStyleBaseline(raw);err!=nil{return nil,ErrRegionInvalid}
  children,err=v2draft.OpaqueDocumentChildren(raw,parent.Contract,parent.Ref);if err!=nil{return nil,ErrRegionInvalid}
 case "ResolvedStyle":
  if _,err:=review.ParseResolvedBinding(raw);err!=nil{return nil,ErrRegionInvalid}
  children,err=v2draft.OpaqueDocumentChildren(raw,parent.Contract,parent.Ref);if err!=nil{return nil,ErrRegionInvalid}
 case "BodyWarpReport":fields=map[string]string{"image":"WorkingImage","poses":"PoseSet","person":"MaskPixels","faces":"FaceLandmarks"}
 default:
  children,err=v2draft.DocumentChildren(raw,parent.Contract,parent.Ref)
  if err!=nil{return nil,ErrRegionInvalid}
 }
 if len(fields)>0{
  var object map[string]json.RawMessage
  if json.Unmarshal(raw,&object)!=nil{return nil,ErrRegionInvalid}
  for field,contract:=range fields{
   value,present:=object[field];if !present{return nil,ErrRegionInvalid}
   if string(value)=="null"&&field=="faces"{continue}
   var ref v2input.Ref
   var shape map[string]json.RawMessage
   if json.Unmarshal(value,&shape)!=nil||len(shape)!=2||json.Unmarshal(value,&ref)!=nil||!regionExecutionRef(ref){return nil,ErrRegionInvalid}
   children=append(children,v2draft.LayerRef{Ref:ref,Contract:contract,ParentRef:parent.Ref,Path:"Document."+field})
  }
 }
 for i:=range children{
  switch children[i].Contract{
  case "WorkingPixels","WorkingAlpha","MaskPixels","DenseWarpField","DefectLabelMap":children[i].Contract="NpyArray"
  case "Artifact":
   // Only schema-defined occurrences receive a type; unknown paths fail.
   key:=children[i].Path
   switch{
   case parent.Contract=="Params"&&key=="Document.image":children[i].Contract="WorkingImage"
   case parent.Contract=="Params"&&key=="Document.source":children[i].Contract="EncodedImage"
   case parent.Contract=="Params"&&key=="Document.plan":children[i].Contract="CompositionPlan"
   case parent.Contract=="Params"&&key=="Document.structure":children[i].Contract="ImageStructure"
   case parent.Contract=="Params"&&key=="Document.resolved":children[i].Contract="ResolvedStyle"
   case parent.Contract=="ResolvedStyle"&&key=="Document.params.image":children[i].Contract="WorkingImage"
   case parent.Contract=="ResolvedStyle"&&key=="Document.params.style":children[i].Contract="StyleV2"
   case parent.Contract=="ResolvedStyle"&&key=="Document.params.stats":children[i].Contract="Stats20"
   case parent.Contract=="ResolvedStyle"&&key=="Document.absolute_lut.artifact":children[i].Contract="NpyArray"
   default:return nil,ErrRegionInvalid
   }
  }
 }
 return children,nil
}
