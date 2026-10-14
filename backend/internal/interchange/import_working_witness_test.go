package interchange
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestImportWorkingWitnessOwnsBytesAndRejectsChangedRole(t *testing.T){
 w:=&ImportWorkingWitnesses{};body:=[]byte(`{"schema_version":1}`)
 master:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)};child:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))},Contract:"WorkingPixels"}
 if err:=w.remember(master,body,[]v2input.Entry{child});err!=nil{t.Fatal(err)}
 body[0]='x';witness,ok:=w.lookup(child);if !ok||rawSHA(witness.body)!=master.SHA256{t.Fatal("caller changed witness")}
 witness.body[0]='x';again,_:=w.lookup(child);if rawSHA(again.body)!=master.SHA256{t.Fatal("returned body aliases cache")}
 child.Contract="WorkingAlpha";if _,ok:=w.lookup(child);ok{t.Fatal("role substitution accepted")}
 if err:=w.remember(master,again.body,[]v2input.Entry{child});err==nil{t.Fatal("conflicting role receipt replaced")}
}
