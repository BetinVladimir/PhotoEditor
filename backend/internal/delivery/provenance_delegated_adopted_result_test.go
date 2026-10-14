package delivery

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/provenance"
)

func TestDelegatedAdoptedResultRejectsMissingScope(t *testing.T){
 reader:=DelegatedAdoptedResultReader{}
 task:=provenance.CanonicalSigningTask{Task:uuid.New(),
  Operation:"render.compose",Version:"1"}
 if _,_,err:=reader.ReadDelegatedAdoptedResult(context.Background(),task);
  err==nil{t.Fatal("unscoped result accepted")}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,_,err:=reader.ReadDelegatedAdoptedResult(ctx,task);
  !errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
}
