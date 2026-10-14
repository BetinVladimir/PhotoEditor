package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestPreparedBackgroundCommitPreservesBytesAndRejectsScopeChanges(t *testing.T){
 for _,mode:=range []string{"valid","id","target","source_version","bytes","media"}{t.Run(mode,func(t *testing.T){
  scope:=preparedPublisherScope();body:=[]byte("exact background encoded bytes")
  source:=ImportBackgroundSource{Style:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))},Portable:"archive-bg",Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Staged:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}}
  object:=objectstore.Object{Key:"staged",VersionID:"v1",SHA256:rawSHA(body),Bytes:int64(len(body)),ContentType:"application/octet-stream"}
  catalog:=&backgroundPublishCatalog{object:object};store:=&preparedImportStoreStub{};objects:=&createStorageObjectsStub{};reader:=&importReaderStub{body:body}
  publisher:=PreparedImportArtifactPublisher{Scope:scope,Store:store,Objects:objects,SourceObjects:reader}
  commit:=PreparedImportBackgroundCommitter{Publisher:publisher,Catalog:catalog,Authority:&backgroundPublishAuthority{}}
  target:=scope.Binding;id:=importBackgroundID(scope.Workflow,source,target).String();media:="image/png"
  switch mode{case "id":id=uuid.NewString();case "target":target.AssetVersionID=uuid.New();case "source_version":catalog.object.VersionID="changed";case "bytes":reader.body=[]byte("substituted");case "media":media="application/octet-stream"}
  got,err:=commit.CommitImportedBackground(context.Background(),source,object,target,id,media)
  if mode=="valid"{if err!=nil||got!=id||store.complete!=1{t.Fatal("publication failed",err)};return}
  if err==nil||got!=""||store.complete!=0{t.Fatal("accepted substitution",err)}
 })}
}
