package objectstore

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/minio/minio-go/v7"
)

func TestInterchangePurgeRequiresEmptyMultipartListing(t *testing.T){
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New())
 for _,mode:=range []string{"empty","neighbor","remaining","failure","nil"}{t.Run(mode,func(t *testing.T){
  calls:=0
  err:=purgeInterchangeKey(context.Background(),key,func(_ context.Context,k string)error{calls++;if k!=key{t.Fatal("wrong key")};return nil},func(_ context.Context,k string)<-chan minio.ObjectMultipartInfo{
   if k!=key{t.Fatal("wrong listing prefix")};if mode=="nil"{return nil}
   rows:=make(chan minio.ObjectMultipartInfo,1)
   switch mode{case "neighbor":rows<-minio.ObjectMultipartInfo{Key:key+".other"};case "remaining":rows<-minio.ObjectMultipartInfo{Key:key};case "failure":rows<-minio.ObjectMultipartInfo{Err:errors.New("listing failed")}}
   close(rows);return rows
  })
  if calls!=1{t.Fatal("purge not called exactly once")}
  if (mode=="empty"||mode=="neighbor")!=(err==nil){t.Fatalf("unexpected result: %v",err)}
 })}
}

func TestInterchangePurgeStopsOnDeletionFailure(t *testing.T){
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New());want:=errors.New("delete failed")
 err:=purgeInterchangeKey(context.Background(),key,func(context.Context,string)error{return want},func(context.Context,string)<-chan minio.ObjectMultipartInfo{t.Fatal("listed after failed delete");return nil})
 if !errors.Is(err,want){t.Fatal(err)}
}

func TestInterchangePurgeRejectsInvalidAndCancelled(t *testing.T){
 var store *Store
 if err:=store.PurgeInterchangeUpload(context.Background(),uuid.Nil,uuid.New());err!=ErrInvalid{t.Fatal(err)}
 if err:=store.PurgeInterchangeUpload(context.Background(),uuid.New(),uuid.New());err!=ErrInvalid{t.Fatal(err)}
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New());ctx,cancel:=context.WithCancel(context.Background());cancel()
 err:=purgeInterchangeKey(ctx,key,func(context.Context,string)error{t.Fatal("IO after cancel");return nil},func(context.Context,string)<-chan minio.ObjectMultipartInfo{return nil})
 if !errors.Is(err,context.Canceled){t.Fatal(err)}
}


func TestArchivePurgeListingCancellationAndOverflow(t *testing.T){
 key,_:=InterchangeUploadKey(uuid.New(),uuid.New())
 for _,mode:=range []string{"cancelled-open-channel","overflow","exact-limit"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  var producer context.Context
  err:=purgeInterchangeKey(ctx,key,func(context.Context,string)error{return nil},func(scan context.Context,_ string)<-chan minio.ObjectMultipartInfo{
   producer=scan
   if mode=="cancelled-open-channel"{cancel();return make(chan minio.ObjectMultipartInfo)}
   count:=10000;if mode=="overflow"{count++}
   rows:=make(chan minio.ObjectMultipartInfo,count)
   for i:=0;i<count;i++{rows<-minio.ObjectMultipartInfo{Key:key+"-foreign"}}
   close(rows);return rows
  })
  if mode=="exact-limit"{if err!=nil{t.Fatal(err)}}else if mode=="overflow"{if err!=ErrUnavailable{t.Fatal(err)}}else if err!=context.Canceled{t.Fatal(err)}
  if producer==nil||producer.Err()!=context.Canceled{t.Fatal("purge producer context leaked")}
 })}
}
