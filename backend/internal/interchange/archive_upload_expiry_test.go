package interchange
import (
 "context"
 "errors"
 "testing"
)
func TestArchiveExpiryFailsClosedAndPropagatesDatabaseResult(t *testing.T){
 if _,err:=(ArchiveUploadExpiry{}).ProcessOne(context.Background());err==nil{t.Fatal("missing DB accepted")}
 db:=&archiveCleanupDB{};ok,err:=(ArchiveUploadExpiry{DB:db}).ProcessOne(context.Background());if err!=nil||!ok||db.query!=`SELECT photo.expire_v30_archive_upload()`||len(db.args)!=0{t.Fatal(ok,err,db)}
 db.err=errors.New("DB failed");if ok,err:=(ArchiveUploadExpiry{DB:db}).ProcessOne(context.Background());ok||!errors.Is(err,db.err){t.Fatal(ok,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel();calls:=db.calls
 if _,err:=(ArchiveUploadExpiry{DB:db}).ProcessOne(ctx);!errors.Is(err,context.Canceled){t.Fatal(err)};if db.calls!=calls{t.Fatal("IO after cancellation")}
}
