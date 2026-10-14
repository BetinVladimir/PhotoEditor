package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)
type archiveDrainStub struct{calls int;err error}
func(s *archiveDrainStub)AssertArchiveDrained(context.Context,uuid.UUID,uuid.UUID,string,string)error{s.calls++;return s.err}
func TestArchiveCleanupAuthorityRequiresLedgerAndPhysicalDrain(t *testing.T){
 lease:=ArchiveCleanupLease{Tenant:uuid.New(),Upload:uuid.New(),Token:uuid.New(),Bucket:"photos"};lease.Key,_=objectstore.InterchangeUploadKey(lease.Tenant,lease.Upload)
 db:=&archiveCleanupDB{};drain:=&archiveDrainStub{};s:=ArchiveCleanupAuthority{DB:db,Bucket:"photos",Drain:drain}
 if err:=s.AssertQuiescent(context.Background(),lease);err!=nil||db.calls!=2||drain.calls!=1{t.Fatal(err,db.calls,drain.calls)}
 drain.err=errors.New("remote writer still active");before:=db.calls
 if err:=s.AssertQuiescent(context.Background(),lease);!errors.Is(err,drain.err)||db.calls!=before+1{t.Fatal("drain error suppressed",err)}
 s.Drain=nil;before=db.calls;if err:=s.AssertQuiescent(context.Background(),lease);err==nil||db.calls!=before{t.Fatal("missing drain authority accepted")}
}
