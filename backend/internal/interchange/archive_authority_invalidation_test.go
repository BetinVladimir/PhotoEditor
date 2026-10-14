package interchange
import (
 "context"
 "errors"
 "testing"
)
func TestArchiveAuthorityInvalidationRequiresDatabaseAndPropagatesResult(t *testing.T){
 if _,err:=(ArchiveAuthorityInvalidation{}).ProcessOne(context.Background());err==nil{t.Fatal("missing DB accepted")}
 db:=&archiveCleanupDB{};changed,err:=(ArchiveAuthorityInvalidation{DB:db}).ProcessOne(context.Background())
 if err!=nil||!changed||db.query!=`SELECT photo.invalidate_v30_archive_authority()`||len(db.args)!=0{t.Fatal(changed,err,db)}
 db.err=errors.New("DB unavailable");if changed,err:=(ArchiveAuthorityInvalidation{DB:db}).ProcessOne(context.Background());changed||!errors.Is(err,db.err){t.Fatal("DB failure hidden",changed,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel();calls:=db.calls
 if _,err:=(ArchiveAuthorityInvalidation{DB:db}).ProcessOne(ctx);!errors.Is(err,context.Canceled){t.Fatal(err)}
 if db.calls!=calls{t.Fatal("cancelled invalidation reached DB")}
}
