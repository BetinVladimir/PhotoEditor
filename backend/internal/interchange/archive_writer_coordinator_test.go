package interchange
import (
 "context"
 "testing"
 "strings"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)
func TestArchiveBeginWriterRequiresInstalledDatabaseAndTicket(t *testing.T){
 s:=SQLArchiveBeginWriters{Bucket:"photos"}
 if _,err:=s.StartBegin(context.Background(),auth.Identity{},uuid.New(),ArchiveUploadIntent{},uuid.New());err==nil{t.Fatal("writer without DB")}
 if err:=s.CloseBegin(context.Background(),ArchiveWriterTicket{},"multipart");err==nil{t.Fatal("closed unowned ticket")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=s.StartBegin(ctx,auth.Identity{},uuid.New(),ArchiveUploadIntent{},uuid.New());err!=context.Canceled{t.Fatal(err)}
}

func TestArchiveCompleteWriterRejectsSubstitutedPhysicalReceipt(t *testing.T){
 db:=&archiveCleanupDB{};s:=SQLArchiveCompleteWriters{DB:db,Bucket:"photos"}
 ticket:=ArchiveWriterTicket{tenant:uuid.New(),upload:uuid.New(),token:uuid.New(),completion:uuid.New(),kind:"complete",size:22};ticket.key,_=objectstore.InterchangeUploadKey(ticket.tenant,ticket.upload)
 pending:=objectstore.PendingObject{Key:ticket.key,Bytes:22,ContentType:"application/zip",VersionID:"v1"}
 if err:=s.CloseComplete(context.Background(),ticket,pending);err!=nil||db.calls!=1{t.Fatal(err)}
 if len(db.args)!=5||db.args[3]!=ticket.completion{t.Fatal("completion lease not bound",db.args)}
 pending.VersionID="null";if err:=s.CloseComplete(context.Background(),ticket,pending);err==nil||db.calls!=1{t.Fatal("closed invalid receipt",err)}
}

func TestArchiveCompleteWriterRejectsMissingLeaseBeforeDatabase(t *testing.T){
 db:=&archiveCleanupDB{};s:=SQLArchiveCompleteWriters{DB:db,Bucket:"photos"}
 ticket:=ArchiveWriterTicket{tenant:uuid.New(),upload:uuid.New(),token:uuid.New(),kind:"complete",size:22};ticket.key,_=objectstore.InterchangeUploadKey(ticket.tenant,ticket.upload)
 pending:=objectstore.PendingObject{Key:ticket.key,Bytes:22,ContentType:"application/zip",VersionID:"v1"}
 if err:=s.CloseComplete(context.Background(),ticket,pending);err==nil||db.calls!=0{t.Fatal("missing completion authority reached DB",err)}
}


func TestArchiveCompleteWriterVersionReceiptContract(t *testing.T){
 for _,version:=range []string{"","null","v\r1","v\n1","v\x001",string([]byte{0xff}),strings.Repeat("a",1025),"v1",strings.Repeat("a",1024)}{t.Run(version,func(t *testing.T){
  db:=&archiveCleanupDB{};s:=SQLArchiveCompleteWriters{DB:db,Bucket:"photos"}
  ticket:=ArchiveWriterTicket{tenant:uuid.New(),upload:uuid.New(),token:uuid.New(),completion:uuid.New(),kind:"complete",size:22}
  ticket.key,_=objectstore.InterchangeUploadKey(ticket.tenant,ticket.upload)
  pending:=objectstore.PendingObject{Key:ticket.key,Bytes:22,ContentType:"application/zip",VersionID:version}
  err:=s.CloseComplete(context.Background(),ticket,pending)
  allowed:=version=="v1"||version==strings.Repeat("a",1024)
  if (err==nil)!=allowed||db.calls!=map[bool]int{false:0,true:1}[allowed]{t.Fatal("invalid physical receipt authority",err,db.calls)}
  if allowed&&db.args[4]!=version{t.Fatal("version identity changed")}
 })}
}
