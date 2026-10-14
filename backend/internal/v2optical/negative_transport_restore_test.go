package v2optical

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type negativeTransportTestWire struct{
 Manifest v2input.Ref `json:"manifest"`
 Chunks []v2input.Ref `json:"chunks"`
 Count int `json:"count"`
 SHA string `json:"sha256"`
}
func negativeTransportRecoveryFixture(t *testing.T,draft NegativeDraft,located LocatedNegativeQualification)(negativeTransportTestWire,v2input.Ref){
 t.Helper()
 qualified,err:=located.Qualification();if err!=nil{t.Fatal(err)}
 plan,params,source,err:=draft.RestorePlan(context.Background(),qualified);if err!=nil{t.Fatal(err)}
 snapshot,_:=draft.Snapshot()
 graph,err:=BuildNegativeTaskTransport(context.Background(),snapshot.Owner.TaskID,plan,source,qualified,params,&negativePublisher{});if err!=nil{t.Fatal(err)}
 return negativeTransportTestWire{graph.Built.Manifest,graph.Built.Chunks,graph.Built.Count,graph.Built.SHA256},params
}
func TestNegativeTransportRecoveryBindsFullDescriptorAndOwnsReturnedData(t *testing.T){
 draft,located,_,_:=negativeParamsFixture(t);qualified,_:=located.Qualification()
 wire,params:=negativeTransportRecoveryFixture(t,draft,located);raw,err:=canonical.Marshal(wire);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","count","entries_sha","chunk_sha","manifest_sha","extra_chunk","unknown","input_alias","transport_alias","unqualified"}{
  t.Run(mode,func(t *testing.T){
   changed:=wire;changed.Chunks=append([]v2input.Ref(nil),wire.Chunks...);q:=qualified
   snapshot,_:=draft.Snapshot()
   switch mode{
   case "count":changed.Count++
   case "entries_sha":changed.SHA=wire.Manifest.SHA256
   case "chunk_sha":changed.Chunks[0].SHA256=wire.Manifest.SHA256
   case "manifest_sha":changed.Manifest.SHA256=wire.SHA
   case "extra_chunk":changed.Chunks=append(changed.Chunks,changed.Chunks[0])
   case "input_alias":changed.Chunks[0].ArtifactID=params.ArtifactID
   case "transport_alias":changed.Manifest.ArtifactID=changed.Chunks[0].ArtifactID
   case "unqualified":q=VerifiedNegativeQualification{}
   }
   if mode=="input_alias"{changed.Manifest.SHA256,_=canonical.Hash(v2input.Manifest{SchemaVersion:1,TaskID:snapshot.Owner.TaskID,EntryCount:changed.Count,EntriesSHA:changed.SHA,Chunks:changed.Chunks})}
   body,_:=canonical.Marshal(changed)
   if mode=="unknown"{var doc map[string]any;json.Unmarshal(body,&doc);doc["unexpected"]=true;body,_=canonical.Marshal(doc)}
   graph,err:=draft.RestorePublishedTransport(context.Background(),body,q,params)
   if mode=="valid"{
    if err!=nil||graph.ID!=snapshot.Owner.TaskID||len(graph.Direct)!=4||graph.Built.Count!=wire.Count{t.Fatal("valid metadata refused",err)}
    graph.Direct[0].ArtifactID=uuid.New();graph.Built.Chunks[0].ArtifactID=uuid.New();body[0]='['
    again,err:=draft.RestorePublishedTransport(context.Background(),raw,qualified,params);if err!=nil||again.Built.Chunks[0]!=wire.Chunks[0]{t.Fatal("caller mutation changed recovery",err)}
    first,_:=again.Open(context.Background());second,_:=again.Open(context.Background())
    a,ok,err:=first.Next(context.Background());if err!=nil||!ok{t.Fatal(err)};b,ok,err:=second.Next(context.Background());if err!=nil||!ok||a!=b{t.Fatal("cursor not independently reopened",err)}
   }else if err==nil||graph.ID!=uuid.Nil{t.Fatal("forged metadata accepted",mode)}
  })
 }
}
