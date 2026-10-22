package maintenance

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)

// Simulates an adapter that ignores cancellation and returns nil anyway.
type cancelledPhysicalCleanup struct { cancel context.CancelFunc }
func (p cancelledPhysicalCleanup) RemoveConfirmed(context.Context,objectstore.Object) error { p.cancel();return nil }
func (p cancelledPhysicalCleanup) PurgeV2Draft(context.Context,uuid.UUID,uuid.UUID) error { p.cancel();return nil }
func (p cancelledPhysicalCleanup) PurgeOriginal(context.Context,uuid.UUID,uuid.UUID) error { p.cancel();return nil }
func (p cancelledPhysicalCleanup) PurgeDocument(context.Context,uuid.UUID,uuid.UUID,uuid.UUID) error { p.cancel();return nil }
func (p cancelledPhysicalCleanup) PurgeAttempt(context.Context,uuid.UUID,uuid.UUID,uuid.UUID,int,int64) error { p.cancel();return nil }
func (p cancelledPhysicalCleanup) AbortMultipart(context.Context,string,string) error { p.cancel();return objectstore.ErrMissing }

func TestPhysicalCleanupCancellationNeverReleasesMetadata(t *testing.T) {
 for _,family:=range []string{"staging","original","document","output","upload","draft"} {
  t.Run(family,func(t *testing.T){
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   objects:=cancelledPhysicalCleanup{cancel}
   var changed,finished bool
   var err error
   switch family {
   case "staging":
    repo:=&stagingPorts{ticket:&StagingCleanup{ID:uuid.New(),Token:uuid.New(),TenantID:uuid.New(),ArtifactID:uuid.New(),Bucket:"bucket",Object:objectstore.Object{Key:"staging/object",VersionID:"v1",SHA256:strings.Repeat("a",64),Bytes:10,ContentType:"image/jpeg"}}}
    changed,err=(StagingCleaner{repo,objects,"bucket"}).CleanOne(ctx);finished=repo.finished
   case "original":
    repo:=&originalCleanupPorts{ticket:&OriginalCleanup{uuid.New(),uuid.New(),uuid.New(),"bucket"}}
    changed,err=(OriginalCleaner{repo,objects,"bucket"}).CleanOne(ctx);finished=repo.finished
   case "document":
    repo:=&documentRepo{ticket:&DocumentCleanup{uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),"bucket"}}
    changed,err=(DocumentCleaner{repo,objects,"bucket"}).CleanOne(ctx);finished=repo.finished
   case "output":
    ticket:=&OutputCleanup{UploadID:uuid.New(),Token:uuid.New(),TenantID:uuid.New(),TaskID:uuid.New(),ArtifactID:uuid.New(),Attempt:1,Fence:1,Bucket:"bucket"}
    ticket.Key,_=objectstore.AttemptKey(ticket.TenantID,ticket.TaskID,ticket.ArtifactID,1,1)
    repo:=&cleanupRepo{ticket:ticket}
    changed,err=(OutputCleaner{repo,objects,"bucket"}).CleanOne(ctx);finished=repo.finished!=0
   case "draft":
    repo:=&draftUploadRepo{ticket:&V2DraftUploadTicket{TenantID:uuid.New(),DocumentID:uuid.New(),ArtifactID:uuid.New(),Token:uuid.New(),Bucket:"bucket"}}
    changed,err=(V2DraftUploadCleaner{repo,objects,"bucket"}).CleanOne(ctx);finished=repo.finished
   case "upload":
    repo:=&uploadCleanupPorts{ticket:uploadCleanupFixture()}
    changed,err=(UploadCleaner{repo,objects}).CleanOne(ctx);finished=repo.finished
   }
   if !errors.Is(err,context.Canceled) || changed || finished { t.Fatalf("changed=%v finished=%v err=%v",changed,finished,err) }
  })
 }
}
