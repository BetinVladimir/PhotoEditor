package v2optical

import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// A protocol-level fixture with a distinct replacement task ID. This exercises
// byte/hash reconstruction, not physical replacement admission qualification.
func TestReplacementTransportHashVerifierBindsTaskAndOwnsCursor(t *testing.T){
 draft,located,_,_:=negativeParamsFixture(t)
 qualified,err:=located.Qualification();if err!=nil{t.Fatal(err)}
 snapshot,err:=draft.Snapshot();if err!=nil{t.Fatal(err)}
 published:=v2input.Ref{ArtifactID:snapshot.ParamsDocument,SHA256:snapshot.ParamsSHA}
 original,err:=draft.Descriptor(context.Background(),qualified,published);if err!=nil{t.Fatal(err)}
 var frozen struct{SchemaVersion int `json:"schema_version"`;Task v2input.DraftTask `json:"task"`;Entries []v2input.Entry `json:"entries"`;ReservedBytes int64 `json:"reserved_bytes"`}
 if err=json.Unmarshal(original,&frozen);err!=nil{t.Fatal(err)}
 oldTask:=frozen.Task.ID;frozen.Task.ID=uuid.New()
 frozen.Task.InputDigest,err=negativeDraftInputDigest(frozen.Task.ID,frozen.Entries);if err!=nil{t.Fatal(err)}
 descriptor,err:=canonical.Marshal(frozen);if err!=nil{t.Fatal(err)}
 entriesSHA,err:=canonical.Hash(frozen.Entries);if err!=nil{t.Fatal(err)}
 chunkSHA,err:=canonical.Hash(v2input.Chunk{SchemaVersion:1,Entries:frozen.Entries});if err!=nil{t.Fatal(err)}
 chunks:=[]v2input.Ref{{ArtifactID:uuid.New(),SHA256:chunkSHA}}
 manifest:=v2input.Manifest{SchemaVersion:1,TaskID:frozen.Task.ID,EntryCount:len(frozen.Entries),EntriesSHA:entriesSHA,Chunks:chunks}
 manifestSHA,err:=canonical.Hash(manifest);if err!=nil{t.Fatal(err)}
 wire:=struct{Manifest v2input.Ref `json:"manifest"`;Chunks []v2input.Ref `json:"chunks"`;Count int `json:"count"`;SHA string `json:"sha256"`}{v2input.Ref{ArtifactID:uuid.New(),SHA256:manifestSHA},chunks,len(frozen.Entries),entriesSHA}
 raw,err:=canonical.Marshal(wire);if err!=nil{t.Fatal(err)}
 loader:=func(context.Context)([]byte,error){return append([]byte(nil),descriptor...),nil}
 graph,err:=restoreNegativePublishedTransport(context.Background(),raw,loader)
 if err!=nil||graph.ID!=frozen.Task.ID||graph.ID==oldTask||len(graph.Direct)!=4{t.Fatal("replacement protocol restore",err)}
 cursor,err:=graph.Open(context.Background());if err!=nil{t.Fatal(err)}
 first,ok,err:=cursor.Next(context.Background());if err!=nil||!ok||first!=frozen.Entries[0]{t.Fatal("cursor entry",err)}
 first.Ref.ArtifactID=uuid.New()
 again,err:=graph.Open(context.Background());if err!=nil{t.Fatal(err)}
 next,ok,err:=again.Next(context.Background());if err!=nil||!ok||next!=frozen.Entries[0]{t.Fatal("cursor storage aliased",err)}
 for _,mode:=range []string{"count","entries","chunk","original_task"}{
  changed:=wire;changed.Chunks=append([]v2input.Ref(nil),wire.Chunks...)
  switch mode{
  case "count":changed.Count++
  case "entries":changed.SHA="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  case "chunk":changed.Chunks[0].SHA256="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  case "original_task":oldManifest:=manifest;oldManifest.TaskID=oldTask;changed.Manifest.SHA256,_=canonical.Hash(oldManifest)
  }
  body,err:=canonical.Marshal(changed);if err!=nil{t.Fatal(err)}
  if _,err:=restoreNegativePublishedTransport(context.Background(),body,loader);err==nil{t.Fatal("changed transport accepted",mode)}
 }
}

func TestReplacementTransportReadRefusesIncompletePackageBeforeSQL(t *testing.T){
 db:=&negativeDraftStoreDB{};prepared:=PreparedNegativeReplacementInputs{bucket:"photos"}
 graph,err:=prepared.ReadPublishedNegativeReplacementTransport(context.Background(),db,"photos",nil,nil,nil,nil,NegativeReplacementDraft{raw:"{}"},uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{},v2input.Ref{})
 if err==nil||graph.ID!=uuid.Nil||db.begins!=0{t.Fatal("incomplete recovery reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=prepared.ReadPublishedNegativeReplacementTransport(ctx,db,"photos",nil,nil,nil,nil,NegativeReplacementDraft{},uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{},v2input.Ref{});err!=context.Canceled{t.Fatal("cancellation",err)}
}
