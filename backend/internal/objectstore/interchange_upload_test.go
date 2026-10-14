package objectstore
import (
 "context"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
)
func TestInterchangeUploadNamespaceAndBounds(t *testing.T){
 key,err:=InterchangeUploadKey(uuid.New(),uuid.New());if err!=nil||!validInterchangeUploadKey(key){t.Fatal("invalid generated key",err)}
 for _,value:=range []string{"tenants/foreign/interchange-uploads/foreign/archive.zip",key+"/extra",strings.Replace(key,"interchange-uploads","uploads",1),strings.ToUpper(key)}{if validInterchangeUploadKey(value){t.Fatal("accepted foreign namespace",value)}}
 var store *Store
 if _,err:=store.CompleteInterchangeMultipart(context.Background(),key,"upload",MaxInterchangeArchiveBytes+1,nil);err!=ErrInvalid{t.Fatal("accepted oversized completion",err)}
 if _,err:=store.RecoverInterchangeMultipart(context.Background(),key,21);err!=ErrInvalid{t.Fatal("accepted short ZIP",err)}
 if _,err:=store.VerifyInterchangePending(context.Background(),PendingObject{Key:key,ContentType:"application/zip",Bytes:22},"");err!=ErrInvalid{t.Fatal("accepted missing mandatory SHA",err)}
}

func TestInterchangePartSigningHorizonAndDeclaredParts(t *testing.T){
 now:=time.Date(2026,10,11,0,0,0,0,time.UTC)
 ttl,err:=interchangePartsTTL(now,now.Add(time.Hour),PartBytes+1,[]int{2,1})
 if err!=nil||ttl!=15*time.Minute{t.Fatal("invalid signing cap",ttl,err)}
 ttl,err=interchangePartsTTL(now,now.Add(2*time.Minute),22,[]int{1})
 if err!=nil||ttl!=119*time.Second{t.Fatal("expiry margin missing",ttl,err)}
 for _,parts:=range [][]int{nil,{0},{2},{1,1}}{if _,err:=interchangePartsTTL(now,now.Add(time.Hour),22,parts);err!=ErrInvalid{t.Fatal("accepted invalid parts",parts,err)}}
 for _,deadline:=range []time.Time{{},now,now.Add(time.Minute)}{if _,err:=interchangePartsTTL(now,deadline,22,[]int{1});err!=ErrInvalid{t.Fatal("accepted exhausted horizon",deadline,err)}}
 if _,err:=interchangePartsTTL(now,now.Add(time.Hour),MaxInterchangeArchiveBytes+1,[]int{1});err!=ErrInvalid{t.Fatal("accepted excessive archive",err)}
}
func TestInterchangePartSigningStopsOnCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel();var store *Store
 if _,err:=store.SignInterchangeParts(ctx,uuid.New(),uuid.New(),"multipart",22,[]int{1},time.Now().Add(time.Hour));err!=context.Canceled{t.Fatal(err)}
 if _,err:=store.BeginInterchangeMultipart(context.Background(),uuid.Nil,uuid.New());err!=ErrInvalid{t.Fatal(err)}
}

func TestInterchangePartURLPublicEnvelope(t *testing.T){
 now:=time.Date(2026,10,11,0,0,0,0,time.UTC)
 good:=PartURL{PartNumber:1,URL:"https://storage.example/archive?signature=opaque",ExpiresAt:now.Add(time.Minute)}
 if !ValidInterchangePartURL(good,now,now.Add(time.Minute)){t.Fatal("valid envelope rejected")}
 local:=good;local.URL="http://127.0.0.1:9443/archive?signature=opaque"
 if !ValidInterchangePartURL(local,now,now.Add(time.Minute)){t.Fatal("local development URL rejected")}
 for _,mutate:=range []func(*PartURL){
  func(p *PartURL){p.URL="http://storage.example/archive?signature=opaque"},
  func(p *PartURL){p.URL="javascript:alert(1)"},
  func(p *PartURL){p.URL="https://user:secret@storage.example/archive"},
  func(p *PartURL){p.URL="https://storage.example/archive#fragment"},
  func(p *PartURL){p.URL="https:///archive"},
  func(p *PartURL){p.URL="https://storage.example/\narchive"},
  func(p *PartURL){p.URL=strings.Repeat("a",32769)},
  func(p *PartURL){p.ExpiresAt=now.Add(5*time.Second)},
  func(p *PartURL){p.ExpiresAt=now.Add(15*time.Minute+time.Second)},
  func(p *PartURL){p.PartNumber=4097},
 }{bad:=good;mutate(&bad);if ValidInterchangePartURL(bad,now,time.Time{}){t.Fatal("accepted invalid link")}}
 if ValidInterchangePartURL(good,now,now.Add(30*time.Second)){t.Fatal("exceeded durable authority horizon")}
}
