package interchange

import (
 "context"
 "testing"
 "sort"
 "photoeditor/backend/internal/canonical"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)
type exportScopedDocuments struct{scopes []artifactjson.Scope}
func(p *exportScopedDocuments)Publish(_ context.Context,scope artifactjson.Scope,body []byte)(artifactjson.Ref,error){
 p.scopes=append(p.scopes,scope)
 return artifactjson.Ref{ID:uuid.New(),SHA256:rawSHA(body)},nil
}
func TestScopedExportTransportBindsRequestBeforePublication(t *testing.T){
 tid,who,intent,compiled,ref,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,ref,raw);if err!=nil{t.Fatal(err)}
 // Entries are normally produced by ResolveExportInputInventory. Root-only
 // inventory here exercises binding; full qualification has separate tests.
 entries:=compiled.InputRoots()
 inventory:=ExportInputInventory{task:compiled.TaskID(),entries:entries}
 documents:=&exportScopedDocuments{}
 for _,mode:=range []string{"task","root","digest","key"}{t.Run(mode,func(t *testing.T){
  bad:=inventory;bad.entries=append([]v2input.Entry(nil),entries...);key:="export-key"
  switch mode{case "task":bad.task=uuid.New();case "root":bad.entries=bad.entries[1:];case "digest":bad.digest="wrong";case "key":key="different-key"}
  if _,err:=PublishScopedExportInputs(context.Background(),who,tid,intent.Request,key,frozen,bad,documents,uuid.Nil);err==nil{t.Fatal("unbound publication accepted")}
  if len(documents.scopes)!=0{t.Fatal("invalid inventory reached storage")}
 })}
}
func TestScopedExportTransportPassesTenantRunTaskLeaseToPublisher(t *testing.T){
 tid,who,intent,compiled,ref,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,ref,raw);if err!=nil{t.Fatal(err)}
 entries:=compiled.InputRoots()
 sort.Slice(entries,func(i,j int)bool{return entries[i].Ref.ArtifactID.String()<entries[j].Ref.ArtifactID.String()})
 digest,err:=canonical.Hash(entries);if err!=nil{t.Fatal(err)}
 inventory:=ExportInputInventory{task:compiled.TaskID(),entries:entries,digest:digest}
 documents:=&exportScopedDocuments{};lease:=uuid.New()
 transport,err:=PublishScopedExportInputs(context.Background(),who,tid,intent.Request,"export-key",frozen,inventory,documents,lease)
 if err!=nil||transport.TaskID()!=compiled.TaskID()||transport.Built().SHA256!=digest{t.Fatal("scoped transport failed",err)}
 if len(documents.scopes)!=2{t.Fatal("wrong transport document count")}
 for index,scope:=range documents.scopes{
  if scope.Tenant!=tid||scope.Run!=intent.Identity.Run||scope.Owner!=intent.Identity.Task||scope.AssemblyLease!=lease{t.Fatal("storage scope changed")}
  expected:="v2_input_chunk_0";if index==1{expected="v2_input_manifest"}
  if scope.Purpose!=expected{t.Fatal("wrong purpose")}
 }
}
