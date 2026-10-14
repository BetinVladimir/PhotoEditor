package interchange
import (
 "context"
 "testing"
 "time"
 "errors"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)
func TestArchivePartNumbersAreOwnedAndBounded(t *testing.T){
 intent,err:=FreezeArchiveUpload(context.Background(),uuid.New(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},"parts",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"archive.zip",Bytes:objectstore.PartBytes+1,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 parts:=[]int{2,1};owned,err:=archivePartNumbers(intent,parts);if err!=nil{t.Fatal(err)};parts[0]=99;if owned[0]!=2{t.Fatal("borrowed input")}
 for _,bad:=range [][]int{nil,{}, {0},{3},{1,1}}{if _,err:=archivePartNumbers(intent,bad);err==nil{t.Fatal("accepted invalid parts",bad)}}
}
func TestArchivePartAuthorityRequiresInstallationAndPermit(t *testing.T){
 s:=SQLArchivePartsAuthority{Bucket:"photos"}
 if _,err:=s.Authorize(context.Background(),auth.Identity{},uuid.New(),ArchiveUploadIntent{},uuid.New(),[]int{1});err==nil{t.Fatal("authorized without DB")}
 if _,err:=(ArchivePartsPermit{}).Sign(context.Background(),nil);err==nil{t.Fatal("signed zero permit")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=s.Authorize(ctx,auth.Identity{},uuid.New(),ArchiveUploadIntent{},uuid.New(),[]int{1});err!=context.Canceled{t.Fatal(err)}
}


type archivePermitSigner struct{calls int;mode string;cancel context.CancelFunc}
func(s *archivePermitSigner)SignInterchangeParts(_ context.Context,_,_ uuid.UUID,_ string,_ int64,parts []int,deadline time.Time)([]objectstore.PartURL,error){
 s.calls++
 urls:=[]objectstore.PartURL{{PartNumber:parts[0],URL:"https://storage.example/signed",ExpiresAt:deadline.Add(-time.Second)}}
 switch s.mode{case "mutate":parts[0]++;case "foreign":urls[0].PartNumber++;case "expired":urls[0].ExpiresAt=time.Now().Add(-time.Second);case "missing":return nil,nil;case "cancel":s.cancel()}
 return urls,nil
}
func TestArchivePermitOwnsSignerBoundary(t *testing.T){
 for _,mode:=range []string{"valid","mutate","foreign","expired","missing","cancel","invalid-id","duplicate","outside","bad-size","expired-permit"}{t.Run(mode,func(t *testing.T){
  tid,upload:=uuid.New(),uuid.New();key,_:=objectstore.InterchangeUploadKey(tid,upload)
  permit:=ArchivePartsPermit{tenant:tid,upload:upload,bucket:"photos",key:key,multipart:"multipart",size:22,parts:[]int{1},notAfter:time.Now().Add(5*time.Minute)}
  switch mode{case "invalid-id":permit.multipart="bad\x00id";case "duplicate":permit.parts=[]int{1,1};case "outside":permit.parts=[]int{2};case "bad-size":permit.size=21;case "expired-permit":permit.notAfter=time.Now().Add(-time.Second)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  signer:=&archivePermitSigner{mode:mode,cancel:cancel}
  urls,err:=permit.Sign(ctx,signer)
  switch mode{
  case "valid":if err!=nil||len(urls)!=1||signer.calls!=1{t.Fatal(urls,err,signer)}
  case "cancel":if !errors.Is(err,context.Canceled)||urls!=nil{t.Fatal(urls,err)}
  case "invalid-id","duplicate","outside","bad-size","expired-permit":if err==nil||urls!=nil||signer.calls!=0{t.Fatal("invalid permit reached signer",urls,err,signer)}
  default:if !errors.Is(err,objectstore.ErrIntegrity)||urls!=nil{t.Fatal("invalid signer receipt escaped",urls,err)}
  }
  if mode=="mutate"&&permit.parts[0]!=1{t.Fatal("signer mutated permit")}
 })}
}

type archiveGenerationPermitSigner struct{legacy,generation int;want uuid.UUID}
func(s *archiveGenerationPermitSigner)SignInterchangeParts(context.Context,uuid.UUID,uuid.UUID,string,int64,[]int,time.Time)([]objectstore.PartURL,error){s.legacy++;return nil,nil}
func(s *archiveGenerationPermitSigner)SignInterchangeGenerationParts(_ context.Context,_,_,generation uuid.UUID,_ string,_ int64,parts []int,deadline time.Time)([]objectstore.PartURL,error){
 s.generation++;if generation!=s.want{return nil,objectstore.ErrIntegrity}
 return []objectstore.PartURL{{PartNumber:parts[0],URL:"https://storage.example/generation",ExpiresAt:deadline.Add(-time.Second)}},nil
}
func TestArchivePermitSignsOnlyBoundGenerationKey(t *testing.T){
 tenantID,uploadID,generationID:=uuid.New(),uuid.New(),uuid.New()
 key,_:=objectstore.InterchangeGenerationKey(tenantID,uploadID,generationID)
 permit:=ArchivePartsPermit{tenant:tenantID,upload:uploadID,bucket:"photos",key:key,multipart:"multipart",size:22,parts:[]int{1},notAfter:time.Now().Add(5*time.Minute)}
 signer:=&archiveGenerationPermitSigner{want:generationID}
 urls,err:=permit.Sign(context.Background(),signer)
 if err!=nil||len(urls)!=1||signer.generation!=1||signer.legacy!=0{t.Fatal(urls,err,signer)}
 if _,err:=permit.Sign(context.Background(),&archivePermitSigner{});err==nil{t.Fatal("legacy-only signer accepted generation")}
 permit.key,_=objectstore.InterchangeGenerationKey(uuid.New(),uploadID,generationID)
 if _,err:=permit.Sign(context.Background(),signer);err==nil||signer.generation!=1{t.Fatal("foreign generation reached signer",err)}
}
