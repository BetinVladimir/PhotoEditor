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

func TestDelegatedTaskDocumentReaderRefusesMissingAuthority(t *testing.T){
 reader:=DelegatedTaskDocumentReader{}
 task:=provenance.CanonicalSigningTask{Task:uuid.New()}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 if _,err:=reader.ReadDelegatedTaskDocument(context.Background(),task,ref,1024);err==nil{
  t.Fatal("missing object authority accepted")
 }
 if _,err:=reader.lookup(context.Background(),task,ref,1024);err==nil{
  t.Fatal("missing SQL scope accepted")
 }
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=reader.ReadDelegatedTaskDocument(ctx,task,ref,1024);
  !errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
}
