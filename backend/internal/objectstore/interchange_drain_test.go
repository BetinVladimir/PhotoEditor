package objectstore
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/minio/minio-go/v7"
)
func TestArchiveMultipartDrainRequiresPositiveRevocationEvidence(t *testing.T){
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New())
 for _,mode:=range []string{"ok","already-absent","still-live","probe-error","abort-error","listed-session","list-error","no-bound-session","cancelled"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  aborted,probed,listed:=0,0,0;multipart:="session";if mode=="no-bound-session"{multipart=""}
  err:=drainInterchangeMultipart(ctx,key,multipart,func(context.Context)error{aborted++;if mode=="abort-error"{return errors.New("transport")};if mode=="already-absent"{return minio.ErrorResponse{Code:"NoSuchUpload"}};return nil},func(context.Context)error{probed++;if mode=="still-live"{return nil};if mode=="probe-error"{return errors.New("transport")};return minio.ErrorResponse{Code:"NoSuchUpload"}},func(context.Context)<-chan minio.ObjectMultipartInfo{listed++;rows:=make(chan minio.ObjectMultipartInfo,1);if mode=="listed-session"{rows<-minio.ObjectMultipartInfo{Key:key}};if mode=="list-error"{rows<-minio.ObjectMultipartInfo{Err:errors.New("transport")}};close(rows);return rows})
  success:=mode=="ok"||mode=="already-absent"||mode=="no-bound-session"
  if (err==nil)!=success{t.Fatal(mode,err)}
  if mode=="no-bound-session"&&(aborted!=0||probed!=0||listed!=1){t.Fatal("unbound drain IO")}
  if mode=="cancelled"&&(aborted+probed+listed!=0){t.Fatal("cancelled drain reached storage")}
 })}
}


func TestArchiveMultipartDrainListingCancellationAndBound(t *testing.T){
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New())
 for _,mode:=range []string{"cancelled-open-channel","overflow","exact-limit","nil-channel"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  var listingContext context.Context
  err:=drainInterchangeMultipart(ctx,key,"",func(context.Context)error{t.Fatal("unbound abort");return nil},func(context.Context)error{t.Fatal("unbound probe");return nil},func(scan context.Context)<-chan minio.ObjectMultipartInfo{
   listingContext=scan
   if mode=="nil-channel"{return nil}
   if mode=="cancelled-open-channel"{cancel();return make(chan minio.ObjectMultipartInfo)}
   count:=10000;if mode=="overflow"{count++}
   rows:=make(chan minio.ObjectMultipartInfo,count)
   for i:=0;i<count;i++{rows<-minio.ObjectMultipartInfo{Key:key+"-foreign"}}
   close(rows);return rows
  })
  switch mode{
  case "cancelled-open-channel":if !errors.Is(err,context.Canceled){t.Fatal(err)}
  case "exact-limit":if err!=nil{t.Fatal(err)}
  default:if !errors.Is(err,ErrUnavailable){t.Fatal(err)}
  }
  if listingContext==nil||!errors.Is(listingContext.Err(),context.Canceled){t.Fatal("listing producer not cancelled on exit")}
 })}
}
