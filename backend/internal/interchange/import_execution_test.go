package interchange

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

type importRegistrarStub struct{calls int;materializer *importMaterializerStub;publisher *transportPublisher;fail error}
func(s *importRegistrarStub)RegisterImportInputs(_ context.Context,t ImportTransport,parsed []byte)error{
 s.calls++
 if s.materializer.calls!=1||s.publisher.calls!=2||t.built.Count!=3||string(parsed)!="{}"{return v2input.ErrInvalid}
 return s.fail
}

func TestImportExecutionPreparesCompleteBlockedInputs(t *testing.T){
 docs,resolver:=importClosureFixture(t);tenant:=uuid.New()
 inventory,err:=ResolveImportInputInventory(context.Background(),tenant,docs,resolver);if err!=nil{t.Fatal(err)}
 _,task:=importMaterializationFixture(t)
 task.ID=docs.TaskID();task.ParamsArtifactID=docs.params.Ref.ArtifactID;task.ParamsSHA=docs.params.Ref.SHA256;task.InputDigest=inventory.InputDigest()
 for _,failure:=range []string{"none","publication","closure","digest","transport","registration"}{t.Run(failure,func(t *testing.T){
  localResolver:=&importNodeResolverStub{nodes:map[uuid.UUID]ImportClosureNode{}}
  for id,node:=range resolver.nodes{localResolver.nodes[id]=node}
  publisher:=&importDocumentPublisherStub{fail:failure=="publication"}
  materializer:=&importMaterializerStub{publisher:publisher}
  transport:=&transportPublisher{badHash:failure=="transport"}
  registrar:=&importRegistrarStub{materializer:materializer,publisher:transport}
  localTask:=task
  if failure=="digest"{localTask.InputDigest=rawSHA([]byte("foreign"))}
  if failure=="closure"{node:=localResolver.nodes[docs.params.Ref.ArtifactID];node.Children=nil;localResolver.nodes[docs.params.Ref.ArtifactID]=node}
  sentinel:=errors.New("registration refused");if failure=="registration"{registrar.fail=sentinel}
  prep:=ImportExecutionPreparation{Documents:publisher,Resolver:localResolver,Materializer:materializer,Transport:transport,Registrar:registrar}
  err:=prep.Prepare(context.Background(),tenant,docs,localTask,[]byte(`{}`))
  if failure=="none"{if err!=nil||registrar.calls!=1{t.Fatal("prepare",err)};return}
  if err==nil{t.Fatal("failed stage accepted")}
  if failure=="publication"||failure=="closure"||failure=="digest"{if materializer.calls!=0||transport.calls!=0||registrar.calls!=0{t.Fatal("later stage executed after refusal")}}
  if failure=="transport"&&registrar.calls!=0{t.Fatal("failed transport registered")}
  if failure=="registration"&&!errors.Is(err,sentinel){t.Fatal("registration error lost",err)}
 })}
}

func TestImportExecutionRejectsForeignParsedBeforePublication(t *testing.T){
 docs,task:=importMaterializationFixture(t);publisher:=&importDocumentPublisherStub{}
 prep:=ImportExecutionPreparation{Documents:publisher,Resolver:&importNodeResolverStub{},Materializer:&importMaterializerStub{},Transport:&transportPublisher{},Registrar:&importRegistrarStub{}}
 if err:=prep.Prepare(context.Background(),uuid.New(),docs,task,[]byte(`{"foreign":true}`));err==nil||len(publisher.calls)!=0{t.Fatal("foreign parsed reached publication")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=prep.Prepare(ctx,uuid.New(),docs,task,[]byte(`{}`));!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
}
