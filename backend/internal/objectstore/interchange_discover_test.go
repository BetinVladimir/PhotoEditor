package objectstore

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/minio/minio-go/v7"
)
func TestDiscoverArchiveMultipartRequiresUniqueExactKey(t *testing.T){
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New())
 for _,mode:=range []string{"one","empty","foreign","duplicate","multiple","invalid","error","nil","cancelled"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  called:=false
  list:=func(context.Context)<-chan minio.ObjectMultipartInfo{
   called=true;if mode=="nil"{return nil}
   rows:=make(chan minio.ObjectMultipartInfo,3)
   switch mode {
   case "one":rows<-minio.ObjectMultipartInfo{Key:key,UploadID:"known"}
   case "foreign":rows<-minio.ObjectMultipartInfo{Key:key+".other",UploadID:"foreign"}
   case "duplicate","multiple":rows<-minio.ObjectMultipartInfo{Key:key,UploadID:"known"};other:="known";if mode=="multiple"{other="other"};rows<-minio.ObjectMultipartInfo{Key:key,UploadID:other}
   case "invalid":rows<-minio.ObjectMultipartInfo{Key:key,UploadID:"bad\x00id"}
   case "error":rows<-minio.ObjectMultipartInfo{Err:errors.New("transport")}
   }
   close(rows);return rows
  }
  if mode=="cancelled"{cancel()}
  receipt,err:=discoverInterchangeMultipart(ctx,key,list)
  switch mode{case "one":if err!=nil||receipt!="known"{t.Fatal(receipt,err)};case "empty","foreign":if err!=nil||receipt!=""{t.Fatal(receipt,err)};default:if err==nil||receipt!=""{t.Fatal("ambiguous discovery accepted")}}
  if mode=="cancelled"&&called{t.Fatal("cancelled discovery called storage")}
 })}
}
func TestDiscoverArchiveMultipartCancellationInterruptsListing(t *testing.T){
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New());ctx,cancel:=context.WithCancel(context.Background())
 _,err:=discoverInterchangeMultipart(ctx,key,func(context.Context)<-chan minio.ObjectMultipartInfo{cancel();return make(chan minio.ObjectMultipartInfo)})
 if !errors.Is(err,context.Canceled){t.Fatal("blocked listing ignored cancellation",err)}
}


func TestDiscoverArchiveMultipartBoundsListingAndCancelsProducer(t *testing.T){
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New())
 for _,mode:=range []string{"limit","ambiguous","transport"}{t.Run(mode,func(t *testing.T){
  var producer context.Context
  receipt,err:=discoverInterchangeMultipart(context.Background(),key,func(ctx context.Context)<-chan minio.ObjectMultipartInfo{
   producer=ctx
   rows:=make(chan minio.ObjectMultipartInfo,10001)
   switch mode{
   case "limit":for i:=0;i<10001;i++{rows<-minio.ObjectMultipartInfo{Key:key+".foreign",UploadID:"foreign"}}
   case "ambiguous":rows<-minio.ObjectMultipartInfo{Key:key,UploadID:"first"};rows<-minio.ObjectMultipartInfo{Key:key,UploadID:"second"}
   case "transport":rows<-minio.ObjectMultipartInfo{Err:errors.New("listing interrupted")}
   }
   close(rows);return rows
  })
  if err==nil||receipt!=""{t.Fatal("incomplete listing established receipt",receipt,err)}
  if producer==nil{t.Fatal("producer not started")}
  select{case <-producer.Done():default:t.Fatal("listing producer not cancelled on refusal")}
 })}
}
