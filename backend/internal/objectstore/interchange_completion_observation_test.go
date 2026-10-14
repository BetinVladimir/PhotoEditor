package objectstore
import (
 "context"
 "testing"
 "strings"
 "github.com/google/uuid"
 "github.com/minio/minio-go/v7"
)
func TestArchiveCompletionObservationRequiresAbsentSessionAndStableVersion(t *testing.T){
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New())
 for _,mode:=range []string{"ok","live","denied","missing","changed","null-version","wrong-key","live-second-probe","live-third-probe","cancelled"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()};probes,reads:=0,0
  result,err:=observeInterchangeCompletion(ctx,key,22,func(context.Context)error{probes++;if mode=="live"||mode=="live-second-probe"&&probes==2||mode=="live-third-probe"&&probes==3{return nil};if mode=="denied"{return minio.ErrorResponse{Code:"AccessDenied"}};return minio.ErrorResponse{Code:"NoSuchUpload"}},func(context.Context)(PendingObject,error){reads++;p:=PendingObject{Key:key,Bytes:22,ContentType:"application/zip",VersionID:"v1"};if mode=="missing"{return PendingObject{},ErrMissing};if mode=="changed"&&reads==2{p.VersionID="v2"};if mode=="null-version"{p.VersionID="null"};if mode=="wrong-key"{p.Key="foreign"};return p,nil})
  if mode=="ok"{if err!=nil||result.VersionID!="v1"||reads!=2||probes!=3{t.Fatal(result,err,reads,probes)}}else if err==nil||result.VersionID!=""{t.Fatal("ambiguous observation accepted",mode,result,err)}
  if mode=="live-third-probe"&&(reads!=2||probes!=3){t.Fatal("late live multipart not observed",reads,probes)}
  if (mode=="live"||mode=="denied"||mode=="cancelled")&&reads!=0{t.Fatal("unproven absence reached object read")}
 })}
}


func TestArchiveCompletionObservationVersionAndLateCancellation(t *testing.T){
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New())
 for _,mode:=range []string{"cr","lf","nul","invalid-utf8","long","boundary","cancel-second-read"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  reads:=0
  got,err:=observeInterchangeCompletion(ctx,key,22,func(context.Context)error{return minio.ErrorResponse{Code:"NoSuchUpload"}},func(context.Context)(PendingObject,error){
   reads++;version:="v1"
   switch mode{
   case "cr":version="v\r1"
   case "lf":version="v\n1"
   case "nul":version="v\x001"
   case "invalid-utf8":version=string([]byte{0xff})
   case "long":version=strings.Repeat("a",1025)
   case "boundary":version=strings.Repeat("a",1024)
   case "cancel-second-read":if reads==2{cancel()}
   }
   return PendingObject{Key:key,Bytes:22,ContentType:"application/zip",VersionID:version},nil
  })
  if mode=="boundary"{if err!=nil||len(got.VersionID)!=1024||reads!=2{t.Fatal(got,err)}}else{
   if err==nil||got!=(PendingObject{}){t.Fatal("invalid or cancelled receipt returned",got,err)}
   if mode=="cancel-second-read"{if err!=context.Canceled||reads!=2{t.Fatal(err,reads)}}else if reads!=1{t.Fatal("invalid version reached second read")}
  }
 })}
}
