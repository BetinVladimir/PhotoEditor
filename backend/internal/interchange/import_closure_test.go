package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type importNodeResolverStub struct{nodes map[uuid.UUID]ImportClosureNode;calls int}
func(s *importNodeResolverStub)ResolveImportNode(_ context.Context,e v2input.Entry)(ImportClosureNode,error){s.calls++;return s.nodes[e.Ref.ArtifactID],nil}
func importClosureFixture(t *testing.T)(ImportPreparedDocuments,*importNodeResolverStub){
 docs,roots:=importInputFixture(t)
 nodes:=map[uuid.UUID]ImportClosureNode{}
 for _,e:=range roots{
  body:=[]byte(`{}`);var children []v2input.Entry
  if e.Contract=="InterchangePlan"{body=docs.plan.Body}
  if e.Contract=="Params"{body=docs.params.Body;children=[]v2input.Entry{roots[0],roots[1]}}
  nodes[e.Ref.ArtifactID]=ImportClosureNode{Entry:e,Body:body,Children:children}
 }
 return docs,&importNodeResolverStub{nodes:nodes}
}
func TestImportClosureTraversesExactRootsOnce(t *testing.T){
 docs,resolver:=importClosureFixture(t)
 inventory,err:=ResolveImportInputInventory(context.Background(),uuid.New(),docs,resolver)
 if err!=nil||len(inventory.entries)!=3||resolver.calls!=3{t.Fatal("closure",err)}
}
func TestImportClosureRejectsOmittedDependenciesBeforeFreeze(t *testing.T){
 docs,resolver:=importClosureFixture(t);node:=resolver.nodes[docs.params.Ref.ArtifactID];node.Children=node.Children[:1];resolver.nodes[docs.params.Ref.ArtifactID]=node
 if _,err:=ResolveImportInputInventory(context.Background(),uuid.New(),docs,resolver);err==nil{t.Fatal("omitted child accepted")}
}
func TestImportClosureRejectsSubstitutedPhysicalBytes(t *testing.T){
 docs,resolver:=importClosureFixture(t);node:=resolver.nodes[docs.plan.Ref.ArtifactID];node.Body=[]byte(`{"foreign":true}`);resolver.nodes[docs.plan.Ref.ArtifactID]=node
 if _,err:=ResolveImportInputInventory(context.Background(),uuid.New(),docs,resolver);err==nil{t.Fatal("substituted bytes accepted")}
}
func TestImportClosureCancellationStopsResolution(t *testing.T){
 docs,resolver:=importClosureFixture(t);ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=ResolveImportInputInventory(ctx,uuid.New(),docs,resolver);err!=context.Canceled||resolver.calls!=0{t.Fatal("cancelled resolution",err)}
}

func TestImportClosureRejectsDocumentsDisguisedAsBinaryLeaves(t *testing.T){
 for _,contract:=range []string{"Params","InterchangePlan","ParsedInterchange"}{
  docs,resolver:=importClosureFixture(t)
  for id,node:=range resolver.nodes{if node.Entry.Contract==contract{node.Body=nil;node.Children=nil;resolver.nodes[id]=node}}
  if _,err:=ResolveImportInputInventory(context.Background(),uuid.New(),docs,resolver);err==nil{t.Fatal("JSON root accepted as binary",contract)}
 }
}
func TestImportClosureRejectsMalformedFrozenRootBeforeObjectResolution(t *testing.T){
 docs,resolver:=importClosureFixture(t);docs.params.Body=[]byte(`{}`)
 if _,err:=ResolveImportInputInventory(context.Background(),uuid.New(),docs,resolver);err==nil||resolver.calls!=0{t.Fatal("malformed local root reached resolver")}
}

func TestImportClosureRejectsMalformedReferenceObjects(t *testing.T){
 for _,raw:=range []string{
  `{"ref":{"artifact_id":"00000000-0000-4000-8000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","extra":true}}`,
  `{"ref":{"artifact_id":"00000000-0000-4000-8000-000000000001"}}`,
  `{"ref":{"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`,
 }{
  if _,err:=importDocumentRefs([]byte(raw),"InterchangePlan");err==nil{t.Fatal("malformed ref disappeared",raw)}
 }
}
func TestImportClosureInlineInventoryExceptionIsPathAndContractBound(t *testing.T){
 raw:=[]byte(`{"manifest":{"inventory":[{"artifact_id":"00000000-0000-4000-8000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","path":"a.CR3","contract":"EncodedImage"}]}}`)
 refs,err:=importDocumentRefs(raw,"ParsedInterchange");if err!=nil||len(refs)!=0{t.Fatal("inline archive IDs treated as readable refs",err)}
 if _,err:=importDocumentRefs(raw,"InterchangePlan");err==nil{t.Fatal("inline exception escaped parsed contract")}
 misplaced:=[]byte(`{"inventory":[{"artifact_id":"00000000-0000-4000-8000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","path":"a.CR3","contract":"EncodedImage"}]}`)
 if _,err:=importDocumentRefs(misplaced,"ParsedInterchange");err==nil{t.Fatal("inline exception escaped exact path")}
}

func TestImportClosurePreservesInlineManifestMaskEvidence(t *testing.T){
 raw:=[]byte(`{"manifest":{"masks":[{"path":"masks/a.npy","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","canvas_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}}`)
 refs,err:=importDocumentRefs(raw,"ParsedInterchange");if err!=nil||len(refs)!=0{t.Fatal("inline mask hash treated as ArtifactRef",err)}
}
