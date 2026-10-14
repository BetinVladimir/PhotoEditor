package interchange
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestPreparedMaskRetainsAllQualifiedParentsAndOwnsSnapshots(t *testing.T){
 w:=&ImportPreparedMaskWitnesses{}
 child:=v2input.Entry{Contract:"MaskPixels",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))}}
 first:=preparedMaskWitness{parent:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))},body:[]byte(`{}`),child:child}
 second:=preparedMaskWitness{parent:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{"mask_refs":[]}`))},body:[]byte(`{"mask_refs":[]}`),contract:"Override",maskBody:[]byte(`{}`),child:child}
 if err:=w.rememberQualified(first);err!=nil{t.Fatal(err)}
 if err:=w.rememberQualified(second);err!=nil{t.Fatal(err)}
 if err:=w.rememberQualified(second);err!=nil{t.Fatal("replay",err)}
 first.body[0]='!';second.maskBody[0]='!'
 all:=w.lookupAll(child)
 if len(all)!=2||all[0].body[0]!='{'||all[1].maskBody[0]!='{'{t.Fatal("parents lost or aliased")}
 all[0].body[0]='!';all[1].maskBody[0]='!'
 fresh:=w.lookupAll(child)
 if fresh[0].body[0]!='{'||fresh[1].maskBody[0]!='{'{t.Fatal("snapshot aliases registry")}
 conflict:=fresh[1];conflict.body=[]byte(`{"changed":true}`);conflict.parent.SHA256=rawSHA(conflict.body)
 if err:=w.rememberQualified(conflict);err==nil{t.Fatal("same parent identity changed bytes")}
 foreign:=fresh[1];foreign.child.Ref.SHA256=rawSHA([]byte("foreign"))
 if err:=w.rememberQualified(foreign);err==nil{t.Fatal("pixel identity changed hash")}
 if len(w.lookupAll(child))!=2{t.Fatal("refusal mutated parents")}
}
func TestPreparedMaskParentOccurrencesAreBounded(t *testing.T){
 w:=&ImportPreparedMaskWitnesses{}
 child:=v2input.Entry{Contract:"MaskPixels",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))}}
 var last preparedMaskWitness
 for i:=0;i<128;i++{last=preparedMaskWitness{parent:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))},body:[]byte(`{}`),child:child};if err:=w.rememberQualified(last);err!=nil{t.Fatal(i,err)}}
 if err:=w.rememberQualified(last);err!=nil{t.Fatal("replay at limit",err)}
 last.parent.ArtifactID=uuid.New()
 if err:=w.rememberQualified(last);err==nil{t.Fatal("unbounded occurrences")}
 if len(w.lookupAll(child))!=128{t.Fatal("limit refusal mutated registry")}
}
// Structural witnesses isolate retention; these bodies do not certify semantics.
func TestPreparedMaskParentStorageSharedAcrossPixels(t *testing.T){
 w:=&ImportPreparedMaskWitnesses{}
 body:=[]byte(`{"mask_refs":[]}`)
 parent:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}
 makeWitness:=func()preparedMaskWitness{return preparedMaskWitness{parent:parent,body:body,contract:"Override",maskBody:[]byte(`{}`),child:v2input.Entry{Contract:"MaskPixels",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))}}}}
 a,b:=makeWitness(),makeWitness()
 if err:=w.rememberQualified(a);err!=nil{t.Fatal(err)}
 if err:=w.rememberQualified(b);err!=nil{t.Fatal(err)}
 if len(w.documents)!=1||w.retainedBytes!=int64(len(body)+4){t.Fatal("parent duplicated or incorrectly accounted")}
 if &w.masks[a.child.Ref.ArtifactID].body[0]!=&w.masks[b.child.Ref.ArtifactID].body[0]{t.Fatal("parent storage not shared")}
 body[0]='!'
 if got:=w.lookupAll(a.child);len(got)!=1||got[0].body[0]!='{'{t.Fatal("caller owns retained bytes")}
 conflict:=b;conflict.child.Ref.ArtifactID=uuid.New();conflict.body=[]byte(`{"changed":true}`);conflict.parent.SHA256=rawSHA(conflict.body)
 if err:=w.rememberQualified(conflict);err==nil{t.Fatal("parent changed through another pixel")}
 if len(w.masks)!=2{t.Fatal("refusal mutated registry")}
}
func TestPreparedMaskRetentionBudgetAtomicAndReplay(t *testing.T){
 w:=&ImportPreparedMaskWitnesses{}
 a:=preparedMaskWitness{parent:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))},body:[]byte(`{}`),child:v2input.Entry{Contract:"MaskPixels",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))}}}
 if err:=w.rememberQualified(a);err!=nil{t.Fatal(err)}
 // Simulate occupied capacity without allocating an image-sized test buffer.
 w.retainedBytes=preparedMaskWitnessByteLimit
 if err:=w.rememberQualified(a);err!=nil{t.Fatal("replay consumes budget",err)}
 b:=a;b.parent.ArtifactID=uuid.New()
 if err:=w.rememberQualified(b);err==nil{t.Fatal("budget not enforced")}
 if len(w.documents)!=1||len(w.lookupAll(a.child))!=1||w.retainedBytes!=preparedMaskWitnessByteLimit{t.Fatal("budget refusal mutated registry")}
}
