package interchange
import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestExportTransportPublishesExactClosureAndOwnsManifestRefs(t *testing.T){
 compiled,resolver:=exportClosureFixture(t)
 inventory,err:=ResolveExportInputInventory(context.Background(),compiled,resolver);if err!=nil{t.Fatal(err)}
 publisher:=&transportPublisher{}
 transport,err:=inventory.PublishInputs(context.Background(),publisher);if err!=nil{t.Fatal(err)}
 built:=transport.Built()
 if transport.TaskID()!=compiled.TaskID()||built.Count!=7||built.SHA256!=inventory.InputDigest()||publisher.calls!=2{t.Fatal("wrong transport",built,publisher.calls)}
 built.Chunks[0].SHA256="changed"
 if transport.Built().Chunks[0].SHA256=="changed"{t.Fatal("chunk alias")}
 entries:=transport.Entries();entries[0].Contract="changed"
 if transport.Entries()[0].Contract=="changed"{t.Fatal("inventory alias")}
}
func TestExportTransportRejectsInputAliasAndMutatedInventory(t *testing.T){
 compiled,resolver:=exportClosureFixture(t)
 inventory,err:=ResolveExportInputInventory(context.Background(),compiled,resolver);if err!=nil{t.Fatal(err)}
 if _,err:=inventory.PublishInputs(context.Background(),&transportPublisher{collision:true,bundle:compiled.planRef.ArtifactID});err==nil{t.Fatal("input alias accepted")}
 inventory.entries[0].Contract="changed"
 if _,err:=inventory.PublishInputs(context.Background(),&transportPublisher{});err==nil{t.Fatal("modified inventory accepted")}
}
func TestExportTransportCancellationBeforePublication(t *testing.T){
 compiled,resolver:=exportClosureFixture(t)
 inventory,err:=ResolveExportInputInventory(context.Background(),compiled,resolver);if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel();publisher:=&transportPublisher{}
 if _,err:=inventory.PublishInputs(ctx,publisher);err!=context.Canceled||publisher.calls!=0{t.Fatal("cancelled publication",err)}
 if _,err:=(ExportInputInventory{}).PublishInputs(context.Background(),publisher);err!=v2input.ErrInvalid{t.Fatal("zero inventory accepted",err)}
}
