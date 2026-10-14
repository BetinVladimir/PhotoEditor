package delivery

import (
 "testing"
 "strings"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestMetadataInventoryExactSnapshotAndPublication(t *testing.T){
 parent:=uuid.New();binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},CanvasHash:strings.Repeat("b",64)}
 _,snapshot,err:=v2input.CompilePinnedSessionInputSnapshot(uuid.New(),[]v2input.SessionSnapshotItem{{Ordinal:0,Binding:binding,WorkingImage:binding.Source,Recipe:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("c",64)}}});if err!=nil{t.Fatal(err)}
 scope:=MetadataSourcePublicationScope{Tenant:uuid.New(),Project:uuid.New(),Actor:uuid.New(),Session:uuid.New(),Binding:binding,Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("d",64)},OriginalBytes:100,OriginalMIME:"image/jpeg"}
 source,_:=canonical.Marshal(MetadataDocument{});id,err:=MetadataSourceIdentity(parent,binding,"copyright",nil);if err!=nil{t.Fatal(err)}
 plan,err:=CompileMetadataSourcePublication(scope,v2input.Ref{ArtifactID:id,SHA256:digest(source)},source,"copyright",nil);if err!=nil{t.Fatal(err)}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(snapshot)}
 raw,pin,err:=CompileMetadataInventory(parent,ref,snapshot,"copyright",nil,[]MetadataSourcePublicationScope{scope},[]MetadataSourcePublication{plan})
 if err!=nil||digest(raw)!=pin{t.Fatal(err)}
 inventory,err:=RestoreMetadataInventory(raw,pin,parent,ref,snapshot,"copyright",nil,[]MetadataSourcePublicationScope{scope},[]MetadataSourcePublication{plan})
 if err!=nil{t.Fatal(err)}
 entry,err:=inventory.Entry(0,binding);if err!=nil||entry.Metadata!=plan.Ref(){t.Fatal(entry,err)}
 exported:=inventory.JCS();exported[0]='!'
 if inventory.JCS()[0]!='{'{t.Fatal("mutable inventory")}
 if _,err:=inventory.Entry(1,binding);err==nil{t.Fatal("foreign ordinal")}
 changed:=binding;changed.CanvasHash=strings.Repeat("e",64)
 if _,err:=inventory.Entry(0,changed);err==nil{t.Fatal("foreign canvas")}
 if _,err:=RestoreMetadataInventory(append(raw,'\n'),digest(append(raw,'\n')),parent,ref,snapshot,"copyright",nil,[]MetadataSourcePublicationScope{scope},[]MetadataSourcePublication{plan});err==nil{t.Fatal("noncanonical inventory")}
 if _,_,err:=(FrozenMetadataInventory{}).CompileChildConfig(scope.Project,FrozenExportRequest{},uuid.New(),plan);err==nil{t.Fatal("unrestored inventory")}
 for _,mode:=range []string{"missing","parent","scope","consent","snapshot"}{
  p:=parent;s:=scope;refs:=[]MetadataSourcePublication{plan};fields:=[]string(nil);doc:=append([]byte(nil),snapshot...)
  switch mode{case "missing":refs=nil;case "parent":p=uuid.New();case "scope":s.Actor=uuid.New();case "consent":fields=[]string{"gps"};case "snapshot":doc[0]='!'}
  if _,_,err:=CompileMetadataInventory(p,ref,doc,"copyright",fields,[]MetadataSourcePublicationScope{s},refs);err==nil{t.Fatal(mode)}
 }
}
