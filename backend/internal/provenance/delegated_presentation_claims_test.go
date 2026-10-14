package provenance

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedPresentationRouterFailsClosed(t *testing.T){
 docs:=delegatedDocumentsFunc(func(context.Context,CanonicalSigningTask,
  v2input.Ref,int)([]byte,error){t.Fatal("unsupported operation read document");return nil,nil})
 producer:=delegatedImageProducerFunc(func(context.Context,v2input.Ref)(uuid.UUID,error){
  t.Fatal("unsupported operation read producer")
  return uuid.Nil,nil
 })
 router:=DelegatedPresentationTaskResolver{Documents:docs,Producers:producer}
 for _,operation:=range []string{"render.compose","optical.negative","unknown"}{
  if _,err:=router.ResolveDelegatedTask(context.Background(),ReadySigningExport{},
   CanonicalSigningTask{Operation:operation,Version:"1"});err==nil{
   t.Fatal("unimplemented delegated claims accepted",operation)
  }
 }
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=router.ResolveDelegatedTask(ctx,ReadySigningExport{},
  CanonicalSigningTask{});!errors.Is(err,context.Canceled){
  t.Fatal("cancelled delegated claims dispatched",err)
 }
}
