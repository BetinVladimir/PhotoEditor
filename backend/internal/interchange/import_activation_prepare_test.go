package interchange
import (
 "context"
 "testing"
 "errors"
 "photoeditor/backend/internal/v2input"
 "github.com/google/uuid"
)
func TestImportActivationRequiresIndependentResourceAuthority(t *testing.T){
 documents,task:=importMaterializationFixture(t)
 receipt,err:=(ImportActivationPreparation{}).Prepare(context.Background(),uuid.New(),documents,task)
 if err==nil||receipt.tenant!=uuid.Nil{t.Fatal("unqualified activation prepared")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err=(ImportActivationPreparation{}).Prepare(ctx,uuid.New(),documents,task)
 if err!=context.Canceled{t.Fatal("cancellation lost",err)}
}


type importActivationResourcesFunc func(context.Context,ImportPreparedDocuments,v2input.DraftTask,ImportInputInventory)error
func(f importActivationResourcesFunc)QualifyImportActivationResources(ctx context.Context,d ImportPreparedDocuments,t v2input.DraftTask,i ImportInputInventory)error{return f(ctx,d,t,i)}

// Mocked typed resolver isolates preparation sequencing and ownership. It does
// not provide native/SQL qualification evidence for any leaf.
func TestImportActivationRechecksClosureResourcesAndOwnsCallbacks(t *testing.T){
 for _,mode:=range []string{"valid","first_resource_refusal","second_resource_refusal","changed_closure","callback_mutation","cancelled_resource"}{t.Run(mode,func(t *testing.T){
  docs,task,_,parsed:=measuredImportFixtureWithParsed(t)
  var params importParamsDocument
  if err:=decodeImportDocument(docs.params.Body,&params);err!=nil{t.Fatal(err)}
  nodes:=map[uuid.UUID]ImportClosureNode{}
  roots:=map[uuid.UUID]string{docs.params.Ref.ArtifactID:"Params",docs.plan.Ref.ArtifactID:"InterchangePlan",docs.evidence.Ref.ArtifactID:"FrozenInterchangeImportEvidence",params.Parsed.ArtifactID:"ParsedInterchange"}
  bodies:=map[uuid.UUID][]byte{docs.params.Ref.ArtifactID:docs.params.Body,docs.plan.Ref.ArtifactID:docs.plan.Body,docs.evidence.Ref.ArtifactID:docs.evidence.Body,params.Parsed.ArtifactID:parsed}
  for id,body:=range bodies{
   refs,err:=importDocumentRefs(body,roots[id]);if err!=nil{t.Fatal(err)}
   children:=[]v2input.Entry{}
   for childID,ref:=range refs{
    contract:=roots[childID];if contract==""{contract="Artifact";nodes[childID]=ImportClosureNode{Entry:v2input.Entry{Contract:contract,Ref:ref}}}
    children=append(children,v2input.Entry{Contract:contract,Ref:ref})
   }
   ref:=v2input.Ref{ArtifactID:id,SHA256:rawSHA(body)}
   nodes[id]=ImportClosureNode{Entry:v2input.Entry{Contract:roots[id],Ref:ref},Body:body,Children:children}
  }
  resolver:=&importNodeResolverStub{nodes:nodes}
  tenant:=uuid.New();ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  inventory,err:=ResolveImportInputInventory(ctx,tenant,docs,resolver);if err!=nil{t.Fatal(err)}
  task.InputDigest=inventory.InputDigest();resolver.calls=0
  calls:=0;denied:=errors.New("resource qualification refused")
  authority:=importActivationResourcesFunc(func(_ context.Context,d ImportPreparedDocuments,current v2input.DraftTask,i ImportInputInventory)error{
   calls++
   if mode=="first_resource_refusal"&&calls==1||mode=="second_resource_refusal"&&calls==2{return denied}
   if mode=="changed_closure"&&calls==1{node:=nodes[docs.plan.Ref.ArtifactID];node.Body=[]byte(`{"changed":true}`);nodes[docs.plan.Ref.ArtifactID]=node}
   if mode=="callback_mutation"{
    d.plan.Body[0]='!';d.params.Body[0]='!';d.evidence.Body[0]='!';current.ExecutionProfile[0]='!';i.entries[0].Ref.SHA256=rawSHA([]byte("other"))
   }
   if mode=="cancelled_resource"{cancel()}
   return nil
  })
  receipt,err:=(ImportActivationPreparation{Resolver:resolver,Resources:authority}).Prepare(ctx,tenant,docs,task)
  success:=mode=="valid"||mode=="callback_mutation"
  if (err==nil)!=success{t.Fatal("unexpected activation qualification",err)}
  if success{
   if calls!=2||receipt.tenant!=tenant||receipt.digest!=task.InputDigest||receipt.documents.plan.Body[0]!='{'||receipt.task.ExecutionProfile[0]!='{'{t.Fatal("receipt ownership or repeated qualification lost")}
  }else if receipt.tenant!=uuid.Nil{t.Fatal("failed preparation returned authority")}
  if mode=="first_resource_refusal"||mode=="changed_closure"||mode=="cancelled_resource"{if calls!=1{t.Fatal("continued after refusal",calls)}}
  if mode=="cancelled_resource"&&err!=context.Canceled{t.Fatal("cancellation lost",err)}
 })}
}
