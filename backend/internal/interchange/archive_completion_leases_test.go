package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)
func TestArchiveCompletionLeasesRequireInstallationAndOwnedIntent(t *testing.T){
 s:=SQLArchiveCompletionLeases{Bucket:"photos"};if _,err:=s.Claim(context.Background());err==nil{t.Fatal("claimed without DB")}
 lease:=ArchiveCompletionLease{Tenant:uuid.New(),Upload:uuid.New(),Token:uuid.New(),Actor:uuid.New(),Session:uuid.New(),MultipartID:"multipart"}
 if _,err:=s.Renew(context.Background(),lease);err==nil{t.Fatal("renewed without intent")}
 if _,err:=s.Finish(context.Background(),lease,objectstore.Object{});err==nil{t.Fatal("finished without intent")}
 ctx,cancel:=context.WithCancel(context.Background());cancel();if _,err:=s.Claim(ctx);err!=context.Canceled{t.Fatal("ignored cancellation",err)}
}
