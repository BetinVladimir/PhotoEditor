package interchange
import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func ownedPreparedMaskWitness(v preparedMaskWitness)preparedMaskWitness{v.body=append([]byte(nil),v.body...);v.maskBody=append([]byte(nil),v.maskBody...);return v}
// Same bytes may legitimately be used by distinct standalone/inline parents.
// Keep each qualified occurrence; no later parent replaces earlier authority.
const preparedMaskWitnessByteLimit int64 = 128 << 20
type preparedMaskParentDocument struct { ref v2input.Ref; contract string; body []byte }
func(w *ImportPreparedMaskWitnesses)rememberQualified(candidate preparedMaskWitness)error{
 if w==nil||!validImportRef(candidate.parent)||!validImportRef(candidate.child.Ref)||candidate.child.Contract!="MaskPixels"||rawSHA(candidate.body)!=candidate.parent.SHA256{return v2input.ErrInvalid}
 if candidate.contract!=""&&candidate.contract!="Override"&&candidate.contract!="Params"{return v2input.ErrInvalid}
 w.mu.Lock();defer w.mu.Unlock()
 id:=candidate.child.Ref.ArtifactID
 first,exists:=w.masks[id]
 if exists {
  if first.child!=candidate.child{return v2input.ErrInvalid}
  all:=append([]preparedMaskWitness{first},w.parents[id]...)
  for _,old:=range all{if old.parent.ArtifactID==candidate.parent.ArtifactID{
   if old.parent!=candidate.parent||old.contract!=candidate.contract||!bytes.Equal(old.body,candidate.body)||!bytes.Equal(old.maskBody,candidate.maskBody){return v2input.ErrInvalid};return nil
  }}
  if len(all)>=128{return v2input.ErrInvalid}
 } else if len(w.masks)>=30000{return v2input.ErrInvalid}
 // Intern only immutable parent bytes. A parent UUID cannot acquire a new
 // hash or role through a different pixel witness.
 document,known:=w.documents[candidate.parent.ArtifactID]
 additional:=int64(len(candidate.maskBody))
 if known {
  if document.ref!=candidate.parent||document.contract!=candidate.contract||!bytes.Equal(document.body,candidate.body){return v2input.ErrInvalid}
 } else {
  if len(w.documents)>=30000{return v2input.ErrInvalid}
  additional+=int64(len(candidate.body))
 }
 if additional>preparedMaskWitnessByteLimit-w.retainedBytes{return v2input.ErrInvalid}
 // All refusals precede mutation, including budget exhaustion.
 if w.masks==nil{w.masks=map[uuid.UUID]preparedMaskWitness{}}
 if !known {
  document=preparedMaskParentDocument{ref:candidate.parent,contract:candidate.contract,body:append([]byte(nil),candidate.body...)}
  if w.documents==nil{w.documents=map[uuid.UUID]preparedMaskParentDocument{}}
  w.documents[candidate.parent.ArtifactID]=document
 }
 candidate.body=document.body
 candidate.maskBody=append([]byte(nil),candidate.maskBody...)
 w.retainedBytes+=additional
 w.revision++
 if !exists{w.masks[id]=candidate;return nil}
 if w.parents==nil{w.parents=map[uuid.UUID][]preparedMaskWitness{}}
 w.parents[id]=append(w.parents[id],candidate);return nil
}
func(w *ImportPreparedMaskWitnesses)lookupAll(entry v2input.Entry)[]preparedMaskWitness{
 if w==nil{return nil};w.mu.RLock();defer w.mu.RUnlock()
 first,ok:=w.masks[entry.Ref.ArtifactID];if !ok||first.child!=entry{return nil}
 result:=[]preparedMaskWitness{ownedPreparedMaskWitness(first)}
 for _,parent:=range w.parents[entry.Ref.ArtifactID]{result=append(result,ownedPreparedMaskWitness(parent))}
 return result
}

// Two full rounds prevent IO for later occurrences from leaving an earlier
// parent unchecked. This is repeated authority, not an atomic storage snapshot.
func resolvePreparedMaskWitnessSet(ctx context.Context,w *ImportPreparedMaskWitnesses,entry v2input.Entry,resolve func(context.Context,preparedMaskWitness)(ExportArrayGeometry,error))(ExportArrayGeometry,error){
 var zero ExportArrayGeometry
 if err:=ctx.Err();err!=nil{return zero,err}
 if w==nil||resolve==nil{return zero,v2input.ErrInvalid}
 w.mu.RLock();revision:=w.revision;w.mu.RUnlock()
 witnesses:=w.lookupAll(entry)
 if len(witnesses)==0{return zero,v2input.ErrInvalid}
 var geometry ExportArrayGeometry
 for round:=0;round<2;round++{for _,witness:=range witnesses{
  if err:=ctx.Err();err!=nil{return zero,err}
  // Callback ownership must not mutate the next round's retained witness.
  actual,err:=resolve(ctx,ownedPreparedMaskWitness(witness));if err!=nil{return zero,err}
  if actual==(ExportArrayGeometry{})||(geometry!=(ExportArrayGeometry{})&&geometry!=actual){return zero,v2input.ErrInvalid}
  geometry=actual
 }}
 w.mu.RLock();unchanged:=w.revision==revision;w.mu.RUnlock()
 if !unchanged{return zero,v2input.ErrInvalid}
 return geometry,ctx.Err()
}
