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

func TestDelegatedDirectProducerOutputBinding(t *testing.T){
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 outputs,_:=json.Marshal([]v2input.Ref{ref})
 task:=provenance.CanonicalSigningTask{Outputs:outputs}
 if !delegatedTaskHasOutput(task,ref){t.Fatal("canonical output lost")}
 ref.ArtifactID=uuid.New()
 if delegatedTaskHasOutput(task,ref){t.Fatal("foreign artifact accepted")}
 if _,err:=(DelegatedDirectImageProducer{}).
  ResolveDelegatedDirectImageProducer(context.Background(),ref);err==nil{
  t.Fatal("missing delegated scope accepted")
 }
}
