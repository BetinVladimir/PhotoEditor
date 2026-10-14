package interchange
import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestInlinePreparedMaskWitnessRequiresQualifiedExactParent(t *testing.T){
 _,_,imageRaw:=importWorkingValidatorFixture(t)
 image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))}
 mask,err:=canonical.Marshal(map[string]any{"artifact":ref,"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"selector":"manual","confidence":1,"protected":true});if err!=nil{t.Fatal(err)}
 parent,err:=canonical.Marshal(map[string]any{"mask_refs":[]any{map[string]any{"mask":json.RawMessage(mask)}}});if err!=nil{t.Fatal(err)}
 entry:=v2input.Entry{Contract:"Override",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(parent)}}
 witnesses:=&ImportPreparedMaskWitnesses{}
 qualifier:=&importMaskQualifierStub{}
 v:=ImportPreparedMaskDocumentValidator{Binding:v2input.SessionVersionBinding{CanvasHash:canvas},Qualifier:qualifier,Witnesses:witnesses}
 if err:=v.rememberInlineOverrideMasks(context.Background(),entry,parent);err!=nil{t.Fatal(err)}
 child:=v2input.Entry{Ref:ref,Contract:"MaskPixels"}
 stored,ok:=witnesses.lookup(child)
 if !ok||stored.contract!="Override"||stored.parent!=entry.Ref||qualifier.calls!=1{t.Fatal("missing inline witness")}
 if err:=validateInlinePreparedMaskWitness(stored.body,stored.maskBody,child);err!=nil{t.Fatal(err)}
 stored.body[0]='!';stored.maskBody[0]='!'
 again,_:=witnesses.lookup(child)
 if err:=validateInlinePreparedMaskWitness(again.body,again.maskBody,child);err!=nil{t.Fatal("witness aliases callback",err)}
 changed:=child;changed.Ref.SHA256=rawSHA([]byte("changed"))
 if err:=validateInlinePreparedMaskWitness(again.body,again.maskBody,changed);err==nil{t.Fatal("foreign pixel hash accepted")}
 if err:=validateInlinePreparedMaskWitness([]byte(`{"mask_refs":[]}`),again.maskBody,child);err==nil{t.Fatal("removed inline mask accepted")}
}
