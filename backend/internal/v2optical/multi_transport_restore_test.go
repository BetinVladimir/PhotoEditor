package v2optical
import (
 "context"
 "encoding/json"
 "sort"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestMultiTransportRestoreChecksEveryChunk(t *testing.T){
 ctx:=context.Background();task:=uuid.New();entries:=make([]v2input.Entry,513)
 for i:=range entries{contract:="NpyArray";if i<3{contract=[]string{"Params","Calibration","Report"}[i]};entries[i]=v2input.Entry{Contract:contract,Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}}
 sort.Slice(entries,func(i,j int)bool{return entries[i].Ref.ArtifactID.String()<entries[j].Ref.ArtifactID.String()})
 descriptor,_:=canonical.Marshal(map[string]any{"task":v2input.DraftTask{ID:task},"entries":entries})
 loader:=func(context.Context)([]byte,error){return descriptor,nil}
 chunks:=make([]v2input.Ref,2)
 for i:=range chunks{end:=(i+1)*512;if end>len(entries){end=len(entries)};sha,_:=canonical.Hash(v2input.Chunk{SchemaVersion:1,Entries:entries[i*512:end]});chunks[i]=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}}
 sha,_:=canonical.Hash(entries);manifestSHA,_:=canonical.Hash(v2input.Manifest{SchemaVersion:1,TaskID:task,EntryCount:len(entries),EntriesSHA:sha,Chunks:chunks})
 wire:=map[string]any{"manifest":v2input.Ref{ArtifactID:uuid.New(),SHA256:manifestSHA},"chunks":chunks,"count":len(entries),"sha256":sha};raw,_:=canonical.Marshal(wire)
 graph,err:=restoreBoundedOpticalPublishedTransport(ctx,raw,loader,3,8);if err!=nil||graph.ID!=task||len(graph.Built.Chunks)!=2{t.Fatal("valid multi transport refused",err)}
 if _,err=restoreOpticalPublishedTransport(ctx,raw,loader,3);err==nil{t.Fatal("single-chunk path accepted multi transport")}
 for _,mutate:=range []func(map[string]any){
 func(p map[string]any){c:=p["chunks"].([]any);c[0],c[1]=c[1],c[0]},
 func(p map[string]any){c:=p["chunks"].([]any);c[1]=c[0]},
 func(p map[string]any){p["chunks"].([]any)[1].(map[string]any)["sha256"]=strings.Repeat("f",64)},
 func(p map[string]any){p["manifest"].(map[string]any)["artifact_id"]=entries[0].Ref.ArtifactID.String()},
 }{var changed map[string]any;json.Unmarshal(raw,&changed);mutate(changed);body,_:=canonical.Marshal(changed);if _,err=restoreBoundedOpticalPublishedTransport(ctx,body,loader,3,8);err==nil{t.Fatal("mutated transport accepted")}}
}
