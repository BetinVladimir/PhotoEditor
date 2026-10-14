package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type snapshotObjectRepositoryStub struct{complete int;badReservation,badCompletion bool}
func(s *snapshotObjectRepositoryStub)ReserveExportSnapshot(_ context.Context,_ auth.Identity,_,_ uuid.UUID,_ string,p CompiledExportSnapshot)(artifactjson.Reservation,error){
 id:=uuid.NewSHA1(p.ID(),[]byte("interchange:export-snapshot:artifact:1"));if s.badReservation{id=uuid.New()};return artifactjson.Reservation{ArtifactID:id,Token:uuid.New()},nil
}
func(s *snapshotObjectRepositoryStub)CompleteExportSnapshot(_ context.Context,_ auth.Identity,_,_ uuid.UUID,_ string,_ CompiledExportSnapshot,r artifactjson.Reservation,o objectstore.Object)(v2input.Ref,error){s.complete++;if s.badCompletion{o.SHA256=rawSHA([]byte("foreign"))};return v2input.Ref{ArtifactID:r.ArtifactID,SHA256:o.SHA256},nil}
func TestExportSnapshotObjectPublisherRecoveryAndIntegrity(t *testing.T){
 _,project,ref,raw,_:=exportPlanFixture(t);snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(ref,raw);if err!=nil{t.Fatal(err)}
 request,err:=canonical.Marshal(ExportSnapshotRequest{Items:snapshot.Items});if err!=nil{t.Fatal(err)}
 tenant:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};compiled,err:=CompileExportSnapshotRequest(context.Background(),tenant,project,who,"key",request);if err!=nil{t.Fatal(err)}
 for _,failure:=range []error{nil,objectstore.ErrExists,objectstore.ErrUnavailable}{
  repository:=&snapshotObjectRepositoryStub{};objects:=&createStorageObjectsStub{failure:failure}
  root,err:=(ExportSnapshotObjectPublisher{Repository:repository,Objects:objects}).PublishExportSnapshot(context.Background(),who,tenant,project,"key",compiled)
  if err!=nil||root.SHA256!=rawSHA(compiled.Body())||repository.complete!=1{t.Fatal("publication failed",root,err)}
  if failure!=nil&&objects.recover!=1{t.Fatal("uncertain write not recovered")}
 }
 for _,failure:=range []string{"reservation","version","completion"}{
  repository:=&snapshotObjectRepositoryStub{badReservation:failure=="reservation",badCompletion:failure=="completion"};objects:=&createStorageObjectsStub{corrupt:failure=="version"}
  root,err:=(ExportSnapshotObjectPublisher{Repository:repository,Objects:objects}).PublishExportSnapshot(context.Background(),who,tenant,project,"key",compiled)
  if err==nil||root.ArtifactID!=uuid.Nil{t.Fatal("accepted corruption",failure)}
  if failure!="completion"&&repository.complete!=0{t.Fatal("committed corrupt object")}
 }
}
