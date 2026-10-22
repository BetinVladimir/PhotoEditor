package interchange

import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/v2input"
)

type exportDiscoveryJSONOnly struct{source *exportNodeResolverStub;binaryCalls int;cause error}
func(s *exportDiscoveryJSONOnly)ResolveExportNode(ctx context.Context,entry v2input.Entry)(ExportClosureNode,error){
 if exportBinaryContract(entry.Contract){s.binaryCalls++;return ExportClosureNode{},s.cause}
 return s.source.ResolveExportNode(ctx,entry)
}

func TestExportGeometryDiscoveryDoesNotAdmitBinaryInputs(t *testing.T){
 compiled,stub:=exportClosureFixture(t)
 cause:=errors.New("native qualifier not installed")
 resolver:=&exportDiscoveryJSONOnly{source:stub,cause:cause}
 parents,err:=DiscoverExportGeometryParents(context.Background(),compiled,resolver)
 if err!=nil||len(parents)!=1||parents[0].Contract!="WorkingImage"||resolver.binaryCalls!=0||stub.calls!=7{t.Fatal("incorrect JSON discovery",parents,err,stub.calls)}
 // These fixtures prove graph discovery only; their {} bodies do not pass
 // real typed WorkingImage schemas. Full admission still requires a native leaf.
 if _,err:=ResolveExportInputInventory(context.Background(),compiled,resolver);!errors.Is(err,cause)||resolver.binaryCalls!=1{t.Fatal("discovery bypassed full physical admission",err)}
}

func TestExportGeometryDiscoveryRejectsBrokenFrozenGraph(t *testing.T){
 for _,mode:=range []string{"body_sha","omitted","mistyped","conflicting_id","cancelled"}{t.Run(mode,func(t *testing.T){
  compiled,stub:=exportClosureFixture(t)
  node:=stub.nodes[compiled.planRef.ArtifactID]
  switch mode{
  case "body_sha":node.Body=[]byte("{}")
  case "omitted":node.Children=node.Children[:len(node.Children)-1]
  case "mistyped":node.Children[0].Contract="Artifact"
  case "conflicting_id":copyChild:=node.Children[0];copyChild.Ref.SHA256=rawSHA([]byte("other"));node.Children=append(node.Children,copyChild)
  }
  stub.nodes[compiled.planRef.ArtifactID]=node
  ctx:=context.Background();if mode=="cancelled"{var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  parents,err:=DiscoverExportGeometryParents(ctx,compiled,&exportDiscoveryJSONOnly{source:stub,cause:errors.New("binary called")})
  if err==nil||parents!=nil{t.Fatal("invalid discovery returned usable parents")}
  if mode=="cancelled"&&stub.calls!=0{t.Fatal("cancelled discovery performed IO")}
 })}
}
