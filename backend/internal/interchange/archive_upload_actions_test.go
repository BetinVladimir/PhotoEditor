package interchange
import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)
type archiveActionStub struct{record ArchiveUploadRecord;authorize,sign,queue,abort int;corrupt bool;mutateAuthority bool;stateDuringSign string}
func(s *archiveActionStub)Lookup(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID)(ArchiveUploadRecord,error){r:=s.record;if r.StorageKey==""{r.StorageKey=r.Intent.ObjectKey()};return r,nil}
func(s *archiveActionStub)Queue(_ context.Context,_ auth.Identity,_ uuid.UUID,_ ArchiveUploadIntent,parts []objectstore.UploadedPart)error{s.queue++;parts[0].ETag="mutated";return nil}
func(s *archiveActionStub)Authorize(_ context.Context,_ auth.Identity,tid uuid.UUID,p ArchiveUploadIntent,_ uuid.UUID,parts []int)(ArchivePartsPermit,error){s.authorize++;if s.mutateAuthority{parts[0]++};return ArchivePartsPermit{tenant:tid,upload:p.ID(),bucket:"photos",key:p.ObjectKey(),multipart:s.record.Reservation.MultipartID,size:p.Request().Bytes,parts:append([]int(nil),parts...),notAfter:time.Now().Add(5*time.Minute)},nil}
func(s *archiveActionStub)SignInterchangeParts(_ context.Context,_,_ uuid.UUID,_ string,_ int64,parts []int,deadline time.Time)([]objectstore.PartURL,error){s.sign++;if s.stateDuringSign!=""{s.record.Reservation.State=s.stateDuringSign};n:=parts[0];if s.corrupt{n++};return []objectstore.PartURL{{PartNumber:n,URL:"https://storage.example/signed",ExpiresAt:deadline.Add(-time.Second)}},nil}
func TestArchiveActionsBindRequestAndOwnCompletionParts(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 p,err:=FreezeArchiveUpload(context.Background(),tid,who,"actions",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:p,Reservation:ArchiveUploadReservation{Token:uuid.New(),State:"uploading",MultipartID:"multipart"}}};s:=ArchiveUploadActions{Repository:stub,Authority:stub,Signer:stub}
 if _,err:=s.Parts(context.Background(),who,tid,pid,p.ID(),"wrong",[]int{1});!errors.Is(err,artifactjson.ErrConflict){t.Fatal(err)}
 if stub.authorize!=0||stub.sign!=0{t.Fatal("signed changed request")}
 if _,err:=s.Parts(context.Background(),who,tid,pid,p.ID(),p.RequestSHA(),[]int{1});err!=nil{t.Fatal(err)}
 stub.corrupt=true;if _,err:=s.Parts(context.Background(),who,tid,pid,p.ID(),p.RequestSHA(),[]int{1});!errors.Is(err,objectstore.ErrIntegrity){t.Fatal("accepted foreign part",err)}
 parts:=[]objectstore.UploadedPart{{PartNumber:1,ETag:"original"}}
 if err:=s.Complete(context.Background(),who,tid,pid,p.ID(),p.RequestSHA(),parts);err!=nil{t.Fatal(err)}
 if parts[0].ETag!="original"||stub.queue!=1{t.Fatal("borrowed completion parts")}
 stub.record.Reservation.State="aborting"
 if err:=s.Complete(context.Background(),who,tid,pid,p.ID(),p.RequestSHA(),parts);!errors.Is(err,artifactjson.ErrStale){t.Fatal(err)}
 if stub.queue!=1{t.Fatal("queued aborting upload")}
}

func(s *archiveActionStub)Abort(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent)error{s.abort++;if s.record.Reservation.State!="aborted"{s.record.Reservation.State="aborting"};return nil}
func TestArchiveAbortRejectsVerifiedAndChangedRequest(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 p,err:=FreezeArchiveUpload(context.Background(),tid,who,"abort",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:p,Reservation:ArchiveUploadReservation{Token:uuid.New(),State:"verified"}}};s:=ArchiveUploadActions{Repository:stub}
 if _,err:=s.Abort(context.Background(),who,tid,pid,p.ID(),p.RequestSHA());!errors.Is(err,artifactjson.ErrStale){t.Fatal(err)}
 stub.record.Reservation.State="uploading"
 if _,err:=s.Abort(context.Background(),who,tid,pid,p.ID(),"changed");!errors.Is(err,artifactjson.ErrConflict){t.Fatal(err)}
 if stub.abort!=0{t.Fatal("invalid abort reached database")}
 if _,err:=s.Abort(context.Background(),who,tid,pid,p.ID(),p.RequestSHA());err!=nil||stub.abort!=1{t.Fatal(err,stub.abort)}
 stub.record.Reservation.State="aborted"
 pending,err:=s.Abort(context.Background(),who,tid,pid,p.ID(),p.RequestSHA());if err!=nil||pending{t.Fatal("already purged reported pending",pending,err)}
}

func TestArchivePartsRejectsAuthorityMutationBeforeSigning(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 p,err:=FreezeArchiveUpload(context.Background(),tid,who,"parts-mutation",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 stub:=&archiveActionStub{mutateAuthority:true,record:ArchiveUploadRecord{Intent:p,Reservation:ArchiveUploadReservation{Token:uuid.New(),State:"uploading",MultipartID:"multipart"}}}
 service:=ArchiveUploadActions{Repository:stub,Authority:stub,Signer:stub}
 parts:=[]int{1}
 urls,err:=service.Parts(context.Background(),who,tid,pid,p.ID(),p.RequestSHA(),parts)
 if !errors.Is(err,objectstore.ErrIntegrity)||len(urls)!=0||stub.sign!=0||stub.authorize!=1||parts[0]!=1{t.Fatal("authority substituted requested part",urls,err,stub,parts)}
}

func TestArchivePartsRejectsAbortDuringURLSigning(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 p,err:=FreezeArchiveUpload(context.Background(),tid,who,"parts-abort-race",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 stub:=&archiveActionStub{stateDuringSign:"aborting",record:ArchiveUploadRecord{Intent:p,Reservation:ArchiveUploadReservation{Token:uuid.New(),State:"uploading",MultipartID:"multipart"}}}
 service:=ArchiveUploadActions{Repository:stub,Authority:stub,Signer:stub}
 urls,err:=service.Parts(context.Background(),who,tid,pid,p.ID(),p.RequestSHA(),[]int{1})
 if !errors.Is(err,artifactjson.ErrStale)||len(urls)!=0||stub.sign!=1{t.Fatal("returned URLs after abort won signing race",urls,err)}
}
