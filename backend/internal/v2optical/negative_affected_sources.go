package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type negativeAffectedSource struct{
 Artifact uuid.UUID `json:"artifact_id"`
 SHA string `json:"sha256"`
 Producer uuid.UUID `json:"producer_task_id"`
 Run uuid.UUID `json:"producer_run_id"`
}
type matchedNegativeAffectedSource struct{Before v2input.SessionVersionBinding;Producer negativeAffectedSource}
func decodeNegativeAffectedSources(raw []byte)([]negativeAffectedSource,error){
 if len(raw)<2||len(raw)>32768{return nil,v2input.ErrInvalid}
 normal,err:=canonical.Bytes(raw);if err!=nil{return nil,v2input.ErrInvalid}
 var out []negativeAffectedSource
 if json.Unmarshal(normal,&out)!=nil||out==nil||len(out)>30{return nil,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(out);if err!=nil||!bytes.Equal(exact,normal){return nil,v2input.ErrInvalid}
 previous:=""
 for _,v:=range out{
  if !validPerspectiveRef(v2input.Ref{ArtifactID:v.Artifact,SHA256:v.SHA})||v.Producer==uuid.Nil||v.Run==uuid.Nil||v.Artifact.String()<=previous{return nil,v2input.ErrInvalid};previous=v.Artifact.String()
 }
 return out,nil
}

// matchNegativeAffectedSources checks every persisted changed source against
// exactly one frozen Params occurrence and affected parent edge. Empty mappings
// are valid for control-only dependencies; this is not a SQL authority receipt.
func matchNegativeAffectedSources(ctx context.Context,operation string,frozen []byte,parents []NegativeReplacementParent,raw []byte)([]matchedNegativeAffectedSource,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 changed,err:=decodeNegativeAffectedSources(raw);if err!=nil{return nil,err}
 bindings,err:=opticalFrozenBindings(operation,frozen);if err!=nil{return nil,err}
 byRef:=map[v2input.Ref]negativeAffectedSource{}
 for _,v:=range changed{
  known:=false;for _,parent:=range parents{if parent.Original==v.Producer&&(parent.Kind=="selected_base"||parent.Kind=="replacement"){known=true}}
  if !known{return nil,v2input.ErrInvalid};byRef[v2input.Ref{ArtifactID:v.Artifact,SHA256:v.SHA}]=v
 }
 used:=map[v2input.Ref]bool{};out:=make([]matchedNegativeAffectedSource,0,len(changed))
 for _,binding:=range bindings{if source,ok:=byRef[binding.Source];ok{
  if used[binding.Source]{return nil,v2input.ErrInvalid};used[binding.Source]=true;out=append(out,matchedNegativeAffectedSource{Before:binding,Producer:source})
 }}
 if len(used)!=len(changed){return nil,v2input.ErrInvalid};return out,ctx.Err()
}

func opticalFrozenBindings(operation string,frozen []byte)([]v2input.SessionVersionBinding,error){
 var bindings []v2input.SessionVersionBinding
 switch operation{
 case "optical.negative":p,err:=ParseNegativeParams(frozen);if err!=nil{return nil,err};bindings=[]v2input.SessionVersionBinding{p.Base}
 case "optical.perspective":p,err:=ParsePerspectiveParams(frozen);if err!=nil{return nil,err};bindings=[]v2input.SessionVersionBinding{p.Base}
 case "optical.panorama":p,err:=ParsePanoramaParams(frozen);if err!=nil{return nil,err};bindings=p.Frames
 case "optical.focus_stack":p,err:=ParseFocusStackParams(frozen);if err!=nil{return nil,err};bindings=p.Frames
 default:return nil,v2input.ErrInvalid
 }
 return bindings,nil
}
