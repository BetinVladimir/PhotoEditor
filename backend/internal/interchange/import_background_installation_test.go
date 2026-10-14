package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)
func TestBackgroundInstallationBuildsPreparedCommitWithoutIO(t *testing.T){
 scope:=preparedPublisherScope()
 old:=scope.Binding;old.AssetVersionID=uuid.New();old.Source.ArtifactID=uuid.New()
 bindings:=[]ImportCorrectionBinding{{Entry:scope.Entry,Old:old,New:scope.Binding}}
 parsed,err:=canonical.Marshal(importParsedValidatorFixture());if err!=nil{t.Fatal(err)}
 catalog:=&backgroundPublishCatalog{};native:=&backgroundPublishNative{};sources:=&backgroundTargetSources{}
 prepared:=&PreparedImportArtifactPublisher{Scope:scope,Store:&preparedImportStoreStub{},Objects:&createStorageObjectsStub{},SourceObjects:&importReaderStub{}}
 installation:=ImportBackgroundInstallation{Catalog:catalog,Native:native,Prepared:prepared}
 remapper,err:=installation.Build(context.Background(),scope.Workflow,parsed,bindings,sources)
 if err!=nil{t.Fatal(err)}
 publisher,ok:=remapper.Publisher.(QualifiedImportBackgroundPublisher);if !ok{t.Fatal("missing qualified publisher")}
 commit,ok:=publisher.Committer.(PreparedImportBackgroundCommitter);if !ok||commit.Publisher.Scope!=scope{t.Fatal("missing scoped prepared commit")}
 prepared.Scope.Binding.AssetVersionID=uuid.New()
 if commit.Publisher.Scope.Binding!=scope.Binding||catalog.calls!=0||native.calls!=0||sources.calls!=0{t.Fatal("mutated config or construction performed IO")}
 installation.Committer=&backgroundPublishCommit{}
 if _,err:=installation.Build(context.Background(),scope.Workflow,parsed,bindings,sources);err==nil{t.Fatal("accepted ambiguous commit configuration")}
 installation.Committer=nil
 if _,err:=installation.Build(context.Background(),scope.Workflow,parsed,bindings,sources);err==nil{t.Fatal("accepted changed scheduler binding")}
}
