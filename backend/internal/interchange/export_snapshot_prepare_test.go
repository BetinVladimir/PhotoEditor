package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type snapshotPreparationFake struct{events []string;checks int;fail string;mutate bool;root v2input.Ref}
func(f *snapshotPreparationFake)CheckExportSnapshot(_ context.Context,_ auth.Identity,_,_ uuid.UUID,p CompiledExportSnapshot)error{
 f.events=append(f.events,"check");f.checks++;if f.fail=="recheck"&&f.checks==2{return errors.New("revoked")};if f.mutate{p.document[0]='!';p.request[0]='!'};return nil
}
func(f *snapshotPreparationFake)PublishExportSnapshot(_ context.Context,_ auth.Identity,_,_ uuid.UUID,_ string,p CompiledExportSnapshot)(v2input.Ref,error){
 f.events=append(f.events,"publish");f.root=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(p.document)};if f.fail=="sha"{f.root.SHA256=rawSHA([]byte("substitution"))};if f.mutate{p.document[0]='!'};return f.root,nil
}
func(f *snapshotPreparationFake)AttachExportSnapshot(_ context.Context,_ auth.Identity,_,_ uuid.UUID,_ string,_ CompiledExportSnapshot,_ v2input.Ref)error{f.events=append(f.events,"attach");if f.fail=="attach"{return errors.New("stale")};return nil}
func TestExportSnapshotPreparationOnlyAcknowledgesAttachedExactRoot(t *testing.T){
 _,project,ref,raw,_:=exportPlanFixture(t);snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(ref,raw);if err!=nil{t.Fatal(err)}
 request,err:=canonical.Marshal(ExportSnapshotRequest{Items:snapshot.Items});if err!=nil{t.Fatal(err)}
 for _,failure:=range []string{"","sha","recheck","attach"}{t.Run(failure,func(t *testing.T){
  f:=&snapshotPreparationFake{fail:failure,mutate:true};p:=ExportSnapshotPreparation{Authority:f,Publication:f,Attachment:f}
  receipt,err:=p.Prepare(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),project,"key",request)
  if failure==""{if err!=nil||receipt.Artifact!=f.root||receipt.ItemCount!=1{t.Fatal("missing receipt",receipt,err)};if len(f.events)!=4||f.events[3]!="attach"{t.Fatal("wrong order",f.events)}}else{if err==nil||receipt.ID!=uuid.Nil{t.Fatal("acknowledged failed preparation",receipt,err)};if failure!="attach"&&f.events[len(f.events)-1]=="attach"{t.Fatal("attached refused root")}}
 })}
}
