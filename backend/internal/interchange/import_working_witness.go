package interchange
import (
 "bytes"
 "sync"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type importPixelWitness struct{master v2input.Ref;body []byte;child v2input.Entry}
// Witnesses are populated only after full canonical WorkingImage validation.
// SQL must still recheck current canonical master and producer provenance.
type ImportWorkingWitnesses struct{mu sync.RWMutex;pixels map[uuid.UUID]importPixelWitness}
func(w *ImportWorkingWitnesses)remember(master v2input.Ref,raw []byte,children []v2input.Entry)error{
 if w==nil{return nil}
 w.mu.Lock();defer w.mu.Unlock()
 if w.pixels==nil{w.pixels=map[uuid.UUID]importPixelWitness{}}
 for _,child:=range children{
  if child.Contract!="WorkingPixels"&&child.Contract!="WorkingAlpha"&&child.Contract!="DenseWarpField"{continue}
  if old,ok:=w.pixels[child.Ref.ArtifactID];ok&&(old.master!=master||old.child!=child||!bytes.Equal(old.body,raw)){return v2input.ErrInvalid}
 }
 for _,child:=range children{
  if child.Contract=="WorkingPixels"||child.Contract=="WorkingAlpha"||child.Contract=="DenseWarpField"{w.pixels[child.Ref.ArtifactID]=importPixelWitness{master:master,body:append([]byte(nil),raw...),child:child}}
 }
 return nil
}
func(w *ImportWorkingWitnesses)lookup(child v2input.Entry)(importPixelWitness,bool){
 if w==nil{return importPixelWitness{},false}
 w.mu.RLock();defer w.mu.RUnlock();value,ok:=w.pixels[child.Ref.ArtifactID]
 if !ok||value.child!=child{return importPixelWitness{},false}
 value.body=append([]byte(nil),value.body...);return value,true
}
func(v ImportScopedDocumentValidator)WorkingWitnesses()*ImportWorkingWitnesses{return v.witnesses}
