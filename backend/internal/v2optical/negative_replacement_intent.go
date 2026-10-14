package v2optical

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

type OpticalReplacementBinding struct {Before v2input.SessionVersionBinding;After v2input.SessionVersionBinding}
// ReboundOpticalIntent owns canonical Params only. It contains no old execution
// profile and grants no source, qualification, resource or queue authority.
type ReboundOpticalIntent struct {operation,raw,digest string}
func(i ReboundOpticalIntent)Operation()string{return i.operation}
func(i ReboundOpticalIntent)Params()[]byte{return []byte(i.raw)}
func(i ReboundOpticalIntent)SHA256()string{return i.digest}

// RebindOpticalIntent preserves frozen user controls and frame ordering while
// replacing exact source bindings. Each replacement must be used exactly once,
// keep its asset-version identity and change the actual immutable source ref.
// Auto perspective requires fresh lines; their original-byte source binding must
// still be verified by the normal qualified perspective compiler afterwards.
func RebindOpticalIntent(ctx context.Context,operation string,frozen []byte,bindings []OpticalReplacementBinding,lines *v2input.Ref)(ReboundOpticalIntent,error){
 var zero ReboundOpticalIntent
 if err:=ctx.Err();err!=nil {return zero,err}
 if len(bindings)<1||len(bindings)>30 {return zero,v2input.ErrInvalid}
 replacements:=make(map[v2input.SessionVersionBinding]v2input.SessionVersionBinding,len(bindings));used:=make(map[v2input.SessionVersionBinding]bool,len(bindings))
 valid:=func(b v2input.SessionVersionBinding)bool{_,err:=domain.ParseDigest(b.CanvasHash);return err==nil&&b.AssetVersionID!=uuid.Nil&&validPerspectiveRef(b.Source)}
 for _,binding:=range bindings {
  if !valid(binding.Before)||!valid(binding.After)||binding.Before.AssetVersionID!=binding.After.AssetVersionID||binding.Before.Source==binding.After.Source {return zero,v2input.ErrInvalid}
  if _,exists:=replacements[binding.Before];exists {return zero,v2input.ErrInvalid};replacements[binding.Before]=binding.After
 }
 replace:=func(b v2input.SessionVersionBinding)(v2input.SessionVersionBinding,error){
  if after,exists:=replacements[b];exists {if used[b] {return b,v2input.ErrInvalid};used[b]=true;return after,nil};return b,nil
 }
 var raw []byte;var digest string;var err error
 switch operation {
 case "optical.negative":
  if lines!=nil {return zero,v2input.ErrInvalid}
  p,e:=ParseNegativeParams(frozen);if e!=nil {return zero,e};p.Base,err=replace(p.Base);if err!=nil {return zero,err};raw,digest,err=p.Compile()
 case "optical.perspective":
  p,e:=ParsePerspectiveParams(frozen);if e!=nil {return zero,e};p.Base,err=replace(p.Base);if err!=nil {return zero,err}
  if p.Mode=="auto" {if lines==nil||!validPerspectiveRef(*lines)||p.Lines==nil||*lines==*p.Lines {return zero,v2input.ErrInvalid};owned:=*lines;p.Lines=&owned} else if lines!=nil {return zero,v2input.ErrInvalid}
  raw,digest,err=p.Compile()
 case "optical.panorama":
  if lines!=nil {return zero,v2input.ErrInvalid}
  p,e:=ParsePanoramaParams(frozen);if e!=nil {return zero,e}
  for i,b:=range p.Frames {p.Frames[i],err=replace(b);if err!=nil {return zero,err}};raw,digest,err=p.Compile()
 case "optical.focus_stack":
  if lines!=nil {return zero,v2input.ErrInvalid}
  p,e:=ParseFocusStackParams(frozen);if e!=nil {return zero,e}
  for i,b:=range p.Frames {p.Frames[i],err=replace(b);if err!=nil {return zero,err}};raw,digest,err=p.Compile()
 default:return zero,v2input.ErrInvalid
 }
 if err!=nil {return zero,err};if len(used)!=len(replacements) {return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil {return zero,err}
 return ReboundOpticalIntent{operation:operation,raw:string(raw),digest:digest},nil
}
