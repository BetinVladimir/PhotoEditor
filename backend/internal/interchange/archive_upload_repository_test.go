package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
)
func TestArchiveReservationReceiptRejectsSubstitution(t *testing.T){
 intent,err:=FreezeArchiveUpload(context.Background(),uuid.New(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},"reserve",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 id,token,state:=intent.ID(),uuid.New(),"reserved"
 if r,err:=validateArchiveReservation("created",&id,&token,&state,nil,intent);err!=nil||r.Token!=token{t.Fatal(r,err)}
 foreign:=uuid.New();multipart:="multipart"
 for _,run:=range []func()error{
  func()error{_,e:=validateArchiveReservation("created",&foreign,&token,&state,nil,intent);return e},
  func()error{_,e:=validateArchiveReservation("created",&id,nil,&state,nil,intent);return e},
  func()error{_,e:=validateArchiveReservation("created",&id,&token,&state,&multipart,intent);return e},
 }{if !errors.Is(run(),objectstore.ErrIntegrity){t.Fatal("accepted corrupt receipt")}}
 for outcome,want:=range map[string]error{"quota":artifactjson.ErrQuota,"conflict":artifactjson.ErrConflict,"stale":artifactjson.ErrStale}{if _,err:=validateArchiveReservation(outcome,nil,nil,nil,nil,intent);!errors.Is(err,want){t.Fatal(outcome,err)}}
 if _,err:=validateArchiveReservation("reconcile",nil,nil,nil,nil,intent);err==nil{t.Fatal("cross-key unresolved Begin accepted")}else if coded,ok:=err.(*domain.Error);!ok||coded.Code!="ARCHIVE_BEGIN_RECONCILIATION_REQUIRED"||coded.HTTPStatus!=409{t.Fatal("wrong cross-key conflict",err)}
 state="uploading";if _,err:=validateArchiveReservation("replayed",&id,&token,&state,&multipart,intent);err!=nil{t.Fatal(err)}
 if _,err:=validateArchiveReservation("created",&id,&token,&state,&multipart,intent);err==nil{t.Fatal("created uploading accepted")}
}
func TestArchiveUploadRepositoryRequiresInstallation(t *testing.T){
 s:=SQLArchiveUploadRepository{Bucket:"photos"};ctx:=context.Background();who:=auth.Identity{};tid:=uuid.New()
 if _,err:=s.Reserve(ctx,who,tid,ArchiveUploadIntent{});err==nil{t.Fatal("reserve without DB")}
 if err:=s.Bind(ctx,who,tid,ArchiveUploadIntent{},uuid.New(),"multipart");err==nil{t.Fatal("bind without DB")}
 if err:=s.Abort(ctx,who,tid,ArchiveUploadIntent{});err==nil{t.Fatal("abort without DB")}
 if err:=s.Queue(ctx,who,tid,ArchiveUploadIntent{},nil);err==nil{t.Fatal("queue without DB")}
 if _,err:=s.Lookup(ctx,who,tid,uuid.New(),uuid.New());err==nil{t.Fatal("lookup without DB")}
 cancelled,cancel:=context.WithCancel(ctx);cancel()
 if _,err:=s.Reserve(cancelled,who,tid,ArchiveUploadIntent{});err!=context.Canceled{t.Fatal(err)}
}
