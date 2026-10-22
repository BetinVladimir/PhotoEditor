package interchange

import (
 "context"
 "testing"
 "photoeditor/backend/internal/canonical"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

type additionalHistoricalRootResolver struct{base *importNodeResolverStub;roots []v2input.Entry}
func(r additionalHistoricalRootResolver)ResolveImportNode(ctx context.Context,e v2input.Entry)(ImportClosureNode,error){return r.base.ResolveImportNode(ctx,e)}
func(r additionalHistoricalRootResolver)AdditionalImportRoots(ctx context.Context)([]v2input.Entry,error){if err:=ctx.Err();err!=nil{return nil,err};return append([]v2input.Entry(nil),r.roots...),nil}
func TestHistoricalReceiptIsIndependentFrozenInputRoot(t *testing.T){
 docs,resolver:=importClosureFixture(t);tenant:=uuid.New()
 before,err:=ResolveImportInputInventory(context.Background(),tenant,docs,resolver);if err!=nil{t.Fatal(err)}
 raw:=[]byte(`{"payload":"signed-opaque-authorization"}`)
 receipt:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"HistoricalOriginReceipt"}
 resolver.nodes[receipt.Ref.ArtifactID]=ImportClosureNode{Entry:receipt,Body:raw,Children:[]v2input.Entry{}}
 after,err:=ResolveImportInputInventory(context.Background(),tenant,docs,additionalHistoricalRootResolver{base:resolver,roots:[]v2input.Entry{receipt}})
 if err!=nil||len(after.entries)!=len(before.entries)+1||after.InputDigest()==before.InputDigest(){t.Fatal("receipt not bound into frozen digest",err)}
 // No invented FaceLandmarks or Params reference was needed for authorization.
 node:=resolver.nodes[receipt.Ref.ArtifactID];node.Body=[]byte(`{"payload":"substituted"}`);resolver.nodes[receipt.Ref.ArtifactID]=node
 if _,err=ResolveImportInputInventory(context.Background(),tenant,docs,additionalHistoricalRootResolver{base:resolver,roots:[]v2input.Entry{receipt}});err==nil{t.Fatal("receipt byte substitution accepted")}
}
func TestHistoricalSignerDeclarationRejectsRelativeOrUnpinnedKeys(t *testing.T){
 raw,_:=importSchedulerConfigFixture(t);var wire importSchedulerOperatorConfig
 if err:=decodeImportDocument(raw,&wire);err!=nil{t.Fatal(err)}
 wire.HistoricalOrigins=&ImportHistoricalSignerFiles{PrivateKeyPath:"relative.seed",PublicSHA:rawSHA([]byte("public")),Issuer:uuid.New(),KeyID:"operator"}
 // ValidateDeclaration checks declaration only; it performs no private-key IO.
 invalid,err:=canonical.Marshal(wire);if err!=nil{t.Fatal(err)}
 if _,err=DecodeImportSchedulerOperatorFiles(invalid,rawSHA(invalid));err==nil{t.Fatal("relative signing seed accepted")}
 wire.HistoricalOrigins.PrivateKeyPath="/operator/historical.seed";valid,err:=canonical.Marshal(wire);if err!=nil{t.Fatal(err)}
 files,err:=DecodeImportSchedulerOperatorFiles(valid,rawSHA(valid));if err!=nil||files.HistoricalOrigins==nil{t.Fatal("valid pinned declaration rejected",err)}
}
func TestHistoricalRootsExcludeRecipeDowngradedToMetadataOnly(t *testing.T){
 parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("parent"))},Contract:"FaceLandmarks"}
 receipt:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("receipt"))},Contract:"HistoricalOriginReceipt"}
 registry:=&ImportPreparedNodeRegistry{auxiliaryParents:map[v2input.Entry]v2input.Entry{receipt:parent}}
 roots,err:=registry.AdditionalImportRootsFor(context.Background(),nil);if err!=nil||len(roots)!=0{t.Fatal("unused receipt escaped into input inventory",err)}
 roots,err=registry.AdditionalImportRootsFor(context.Background(),[]v2input.Entry{parent});if err!=nil||len(roots)!=1||roots[0]!=receipt{t.Fatal("reachable historical parent lost its receipt",err)}
}
