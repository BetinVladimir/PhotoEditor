package v2optical

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// RebindMapped builds all before/after pairs from a scoped persisted source map
// and this ready step's physical parent proofs. No caller-defined source pair
// is accepted. SQL must still lock the map and replacement membership.
func(p PreparedReplacementParentSources)mappedReplacementBindings(ctx context.Context,mapping VerifiedNegativeAffectedSources)([]OpticalReplacementBinding,error){
 var zero []OpticalReplacementBinding
 if err:=ctx.Err();err!=nil{return zero,err}
 a,err:=canonical.Marshal(p.step.node);if err!=nil{return zero,err};b,err:=canonical.Marshal(mapping.step.node)
 if err!=nil||!bytes.Equal(a,b)||p.step.intent.locator!=mapping.step.intent.locator||p.step.intent.raw!=mapping.step.intent.raw||p.step.intent.who.UserID!=mapping.step.intent.who.UserID||p.step.intent.who.SessionID!=mapping.step.intent.who.SessionID{return zero,v2input.ErrInvalid}
 l:=p.step.intent.locator;sum:=sha256.Sum256([]byte(p.step.intent.raw))
 if hex.EncodeToString(sum[:])!=l.Params.SHA256||int64(len(p.step.intent.raw))!=l.Params.Bytes{return zero,v2input.ErrInvalid}
 matched,err:=matchNegativeAffectedSources(ctx,l.Operation,[]byte(p.step.intent.raw),p.step.node.Parents,[]byte(mapping.raw));if err!=nil{return zero,err}
 exact,err:=canonical.Marshal(matched);if err!=nil{return zero,err};owned,err:=canonical.Marshal(mapping.sources)
 if err!=nil||!bytes.Equal(exact,owned){return zero,v2input.ErrInvalid}
 if len(matched)==0{
  // A control-only dependency retains every frozen source and user control.
  return nil,ctx.Err()
 }
 bindings:=make([]OpticalReplacementBinding,0,len(matched))
 for _,entry:=range matched{
  var ref *v2input.Ref;var raw []byte
  for _,parent:=range p.step.node.Parents{if parent.Original==entry.Producer.Producer{
   if parent.Kind=="selected_base"{
    s:=p.selected.selection.source
    if s.Task!=entry.Producer.Producer||s.Run!=entry.Producer.Run{return zero,v2input.ErrInvalid}
    ref=p.selected.selection.SelectedBase();raw=p.selected.selection.WorkingImageBytes()
   }else if parent.Kind=="replacement"{
    for _,proof:=range p.parents{if proof.selection.metadata.Task==parent.Task{r:=proof.selection.SourceRef();ref=&r;raw=proof.selection.WorkingImageBytes()}}
   }
  }}
  if ref==nil{return zero,v2input.ErrInvalid}
  image,err:=review.ParseWorkingImage(raw);if err!=nil{return zero,v2input.ErrInvalid}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return zero,err}
  after:=entry.Before;after.Source=*ref;after.CanvasHash=canvas
  bindings=append(bindings,OpticalReplacementBinding{Before:entry.Before,After:after})
 }
 return bindings,ctx.Err()
}

func(p PreparedReplacementParentSources)RebindMapped(ctx context.Context,mapping VerifiedNegativeAffectedSources,lines *v2input.Ref)(ReboundOpticalIntent,error){
 bindings,err:=p.mappedReplacementBindings(ctx,mapping);if err!=nil{return ReboundOpticalIntent{},err}
 if len(bindings)==0 {
  l:=p.step.intent.locator
  if lines!=nil{
   // Control-only invalidation keeps the immutable source, but auto mode
   // still requires freshly adopted lines. Do not invent an identity mapping.
   if l.Operation!="optical.perspective"{return ReboundOpticalIntent{},v2input.ErrInvalid}
   params,err:=ParsePerspectiveParams([]byte(p.step.intent.raw));if err!=nil{return ReboundOpticalIntent{},err}
   if params.Mode!="auto"||params.Lines==nil||!validPerspectiveRef(*lines)||*params.Lines==*lines{return ReboundOpticalIntent{},v2input.ErrInvalid}
   owned:=*lines;params.Lines=&owned
   raw,sha,err:=params.Compile();if err!=nil{return ReboundOpticalIntent{},err}
   return ReboundOpticalIntent{operation:l.Operation,raw:string(raw),digest:sha},ctx.Err()
  }
  return ReboundOpticalIntent{operation:l.Operation,raw:p.step.intent.raw,digest:l.Params.SHA256},ctx.Err()
 }
 return p.Rebind(ctx,bindings,lines)
}
