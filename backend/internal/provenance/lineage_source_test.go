package provenance

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
type signingExecutionStub struct{graph SigningExecutionGraph;calls,changeAt int}
func(s *signingExecutionStub)ReadSigningExecution(context.Context,auth.Identity,ReadySigningExport)(SigningExecutionGraph,error){s.calls++;graph:=s.graph;graph.Nodes=append([]SigningExecutionNode(nil),graph.Nodes...);if s.changeAt>0&&s.calls>=s.changeAt{graph.Nodes[0].Fence++};return graph,nil}
type signingLineagePublisherStub struct{calls int;wrong bool;mutate bool}
func(s *signingLineagePublisherStub)PublishSigningLineage(_ context.Context,_ auth.Identity,_ ReadySigningExport,expected v2input.Ref,body []byte)(v2input.Ref,error){s.calls++;if s.mutate{body[0]='x'};if s.wrong{expected.ArtifactID=uuid.New()};return expected,nil}
func TestCompiledLineagePublicationRechecksFullCanonicalEvidence(t *testing.T){
 for _,mode:=range []string{"valid","mutate","before","after","receipt","target","cancelled"}{t.Run(mode,func(t *testing.T){
  graph:=signingGraphFixture();execution:=&signingExecutionStub{graph:graph};documents:=&signingLineagePublisherStub{}
  ready:=ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),ExportID:uuid.New(),Task:graph.FinalTask,Ref:graph.Export}
  who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};ctx:=context.Background()
  switch mode{case "mutate":documents.mutate=true;case "before":execution.changeAt=2;case "after":execution.changeAt=3;case "receipt":documents.wrong=true;case "target":ready.Ref.ArtifactID=uuid.New();case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  ref,body,err:=(CompiledExportLineageSource{Execution:execution,Documents:documents}).ResolveFrozenExportLineage(ctx,who,ready)
  if mode=="valid"||mode=="mutate"{
   if err!=nil||execution.calls!=3||documents.calls!=1||ref.SHA256!=bodyDigest(body)||body[0]!='{'{t.Fatal("lineage publication failed/aliased",err)}
   if _,err:=DecodeSigningLineage(body,ref);err!=nil{t.Fatal(err)}
  }else if err==nil||ref.ArtifactID!=uuid.Nil||body!=nil{t.Fatal("stale publication exposed authority")}
  if (mode=="before"||mode=="target"||mode=="cancelled")&&documents.calls!=0{t.Fatal("invalid evidence published")}
 })}
}
