package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/objectstore"
)

type archiveCleanupRow struct {scan func(...any)error}
func(r archiveCleanupRow)Scan(dest ...any)error{return r.scan(dest...)}
type archiveCleanupDB struct {calls int;args []any;query string;err error}
func(d *archiveCleanupDB)QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 d.calls++;d.args=append([]any(nil),args...);d.query=query
 return archiveCleanupRow{scan:func(dest ...any)error{if d.err!=nil{return d.err};*dest[0].(*bool)=true;return nil}}
}
func TestArchiveCleanupAdapterBindsPhysicalScope(t *testing.T){
 db:=&archiveCleanupDB{};s:=SQLArchiveCleanupLeases{DB:db,Bucket:"photos"}
 lease:=ArchiveCleanupLease{Tenant:uuid.New(),Upload:uuid.New(),Token:uuid.New(),Bucket:"photos"};lease.Key,_=objectstore.InterchangeUploadKey(lease.Tenant,lease.Upload)
 ok,err:=s.Finish(context.Background(),lease);if err!=nil||!ok{t.Fatal(ok,err)}
 if db.calls!=1||len(db.args)!=5||db.args[0]!=lease.Tenant||db.args[1]!=lease.Upload||db.args[2]!=lease.Token||db.args[3]!=lease.Bucket||db.args[4]!=lease.Key{t.Fatal("physical scope not bound",db.args)}
 for _,mutate:=range []func(*ArchiveCleanupLease){func(l *ArchiveCleanupLease){l.Bucket="foreign"},func(l *ArchiveCleanupLease){l.Key+=".other"},func(l *ArchiveCleanupLease){l.Upload=uuid.New()},func(l *ArchiveCleanupLease){l.Token=uuid.Nil}}{
  bad:=lease;mutate(&bad);if _,err:=s.Finish(context.Background(),bad);err==nil{t.Fatal("accepted changed scope")}
 }
 if db.calls!=1{t.Fatal("invalid scope reached database")}
}
func TestArchiveCleanupAdapterAbsentCancelledAndDatabaseFailure(t *testing.T){
 s:=SQLArchiveCleanupLeases{Bucket:"photos"};if _,err:=s.Claim(context.Background());err==nil{t.Fatal("missing DB accepted")}
 db:=&archiveCleanupDB{err:pgx.ErrNoRows};s.DB=db
 lease,err:=s.Claim(context.Background());if err!=nil||lease!=nil{t.Fatal(lease,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=s.Claim(ctx);!errors.Is(err,context.Canceled){t.Fatal(err)}
 if db.calls!=1{t.Fatal("cancelled claim reached DB")}
 db.err=errors.New("database unavailable")
 if _,err:=s.Claim(context.Background());!errors.Is(err,db.err){t.Fatal("DB failure suppressed",err)}
}
