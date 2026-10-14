package delivery

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedOriginalRejectsMissingScopeAndCancellation(t *testing.T){
 qualifier:=DelegatedOriginalQualifier{}
 task:=provenance.CanonicalSigningTask{Task:uuid.New(),
  Operation:"asset.decode",Version:"1"}
 original:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 master:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)}
 if _,err:=qualifier.lookup(context.Background(),task,original,master,100);
  err==nil{t.Fatal("missing delegated SQL scope accepted")}
 if err:=qualifier.QualifyDelegatedOriginal(context.Background(),task,
  original,master,100);err==nil{t.Fatal("missing object reader accepted")}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if err:=qualifier.QualifyDelegatedOriginal(ctx,task,original,master,100);
  !errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
}
