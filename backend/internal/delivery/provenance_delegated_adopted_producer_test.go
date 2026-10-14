package delivery

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedAdoptedProducerRequiresSingleRoot(t *testing.T){
 root:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 raw,_:=json.Marshal([]v2input.Ref{root})
 if !delegatedTaskHasOnlyOutput(provenance.CanonicalSigningTask{Outputs:raw},root){
  t.Fatal("single adopted root lost")
 }
 other:=v2input.Ref{ArtifactID:uuid.New(),SHA256:root.SHA256}
 raw,_=json.Marshal([]v2input.Ref{root,other})
 if delegatedTaskHasOnlyOutput(provenance.CanonicalSigningTask{Outputs:raw},root){
  t.Fatal("extra root accepted")
 }
 if _,err:=(DelegatedAdoptedImageProducer{}).
  ResolveDelegatedAdoptedImageProducer(context.Background(),other);err==nil{
  t.Fatal("unbound adopter accepted")
 }
}
