package interchange
import (
 "context"
 "errors"
 "fmt"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)
type archiveProcessorLeaseStub struct{lease ArchiveCompletionLease;renew,finish,release,fail,starts,closes int;deny bool;receipt string}
func(s *archiveProcessorLeaseStub)Claim(context.Context)(*ArchiveCompletionLease,error){return &s.lease,nil}
func(s *archiveProcessorLeaseStub)Renew(context.Context,ArchiveCompletionLease)(bool,error){s.renew++;return !s.deny,nil}
func(s *archiveProcessorLeaseStub)Release(context.Context,ArchiveCompletionLease)(bool,error){s.release++;return true,nil}
func(s *archiveProcessorLeaseStub)Fail(context.Context,ArchiveCompletionLease)(bool,error){s.fail++;return true,nil}
func(s *archiveProcessorLeaseStub)Finish(context.Context,ArchiveCompletionLease,objectstore.Object)(bool,error){s.finish++;return true,nil}
type archiveProcessorObjectsStub struct{recover,verify,complete int;lost,corrupt bool;observeErr error;onVerify func()}
func(s *archiveProcessorObjectsStub)CompleteInterchangeMultipart(_ context.Context,key,_ string,size int64,_ []objectstore.UploadedPart)(objectstore.PendingObject,error){s.complete++;if s.lost{return objectstore.PendingObject{},objectstore.ErrUnavailable};return objectstore.PendingObject{Key:key,Bytes:size,ContentType:"application/zip",VersionID:"v1"},nil}
func(s *archiveProcessorObjectsStub)ObserveInterchangeCompletion(_ context.Context,key,_ string,size int64)(objectstore.PendingObject,error){s.recover++;if s.observeErr!=nil{return objectstore.PendingObject{},s.observeErr};return objectstore.PendingObject{Key:key,Bytes:size,ContentType:"application/zip",VersionID:"v1"},nil}
func(s *archiveProcessorObjectsStub)VerifyInterchangePending(_ context.Context,p objectstore.PendingObject,sha string)(objectstore.Object,error){s.verify++;if s.onVerify!=nil{s.onVerify()};if s.corrupt{sha=strings.Repeat("b",64)};return objectstore.Object{Key:p.Key,VersionID:p.VersionID,Bytes:p.Bytes,ContentType:p.ContentType,SHA256:sha},nil}
func TestArchiveCompletionProcessorRecoveryAndFencing(t *testing.T){
 tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"key",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"bundle.zip",Bytes:22,SHA256:strings.Repeat("a",64)});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"ok","lost","corrupt","lease-lost","closed-receipt","invalid-receipt"}{t.Run(mode,func(t *testing.T){
  leases:=&archiveProcessorLeaseStub{lease:ArchiveCompletionLease{Tenant:tid,Upload:intent.ID(),Token:uuid.New(),Actor:who.UserID,Session:who.SessionID,Intent:intent,StorageKey:intent.ObjectKey(),MultipartID:"multipart",Parts:[]objectstore.UploadedPart{{PartNumber:1,ETag:"etag"}}},deny:mode=="lease-lost"};objects:=&archiveProcessorObjectsStub{lost:mode=="lost",corrupt:mode=="corrupt"}
  if mode=="closed-receipt"{leases.receipt="v1"};if mode=="invalid-receipt"{leases.receipt="null"}
  ok,err:=(ArchiveCompletionProcessor{Leases:leases,Objects:objects,Writers:leases}).ProcessOne(context.Background())
  if mode=="ok"||mode=="lost"||mode=="closed-receipt"{if err!=nil||!ok||leases.finish!=1||leases.release!=0{t.Fatal("completion failed",err)}}else if mode=="corrupt"{if err!=nil||!ok||leases.fail!=1||leases.finish!=0||leases.release!=0{t.Fatal("integrity failure not durably fenced",err)}}else{if err==nil||ok||leases.finish!=0||leases.release!=1{t.Fatal("accepted invalid completion",err)}}
  if mode=="closed-receipt"&&(leases.starts!=0||leases.closes!=0||objects.complete!=0||objects.recover!=0||objects.verify!=1){t.Fatal("closed receipt repeated physical write")}
  if mode!="lease-lost"&&mode!="closed-receipt"&&mode!="invalid-receipt"&&(leases.starts!=1||leases.closes!=1){t.Fatal("writer ticket lifecycle missing")}
  if mode=="invalid-receipt"&&(objects.complete!=0||objects.verify!=0||leases.starts!=0){t.Fatal("invalid recovered receipt reached IO")}
  if mode=="lost"&&objects.recover!=1{t.Fatal("did not recover lost response")}
  if mode=="lease-lost"&&(!errors.Is(err,ErrArchiveCompletionLeaseLost)||objects.complete!=0){t.Fatal("lost lease reached object IO")}
 })}
}

func TestArchiveCompletionRejectsClaimWithForeignUploadBeforeObjectIO(t *testing.T){
 tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"foreign-claim",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"bundle.zip",Bytes:22,SHA256:strings.Repeat("a",64)});if err!=nil{t.Fatal(err)}
 leases:=&archiveProcessorLeaseStub{lease:ArchiveCompletionLease{Tenant:tid,Upload:uuid.New(),Token:uuid.New(),Actor:who.UserID,Session:who.SessionID,Intent:intent,StorageKey:intent.ObjectKey(),MultipartID:"multipart",Parts:[]objectstore.UploadedPart{{PartNumber:1,ETag:"etag"}}}}
 objects:=&archiveProcessorObjectsStub{}
 ok,err:=(ArchiveCompletionProcessor{Leases:leases,Writers:leases,Objects:objects}).ProcessOne(context.Background())
 if ok||!errors.Is(err,objectstore.ErrIntegrity)||leases.renew!=0||leases.starts!=0||leases.finish!=0||leases.release!=1||objects.complete!=0||objects.recover!=0||objects.verify!=0{t.Fatal("foreign claim reached completion IO",ok,err,leases,objects)}
}

func(s *archiveProcessorLeaseStub)StartComplete(_ context.Context,lease ArchiveCompletionLease)(ArchiveWriterTicket,error){s.starts++;return ArchiveWriterTicket{tenant:lease.Tenant,upload:lease.Upload,token:uuid.New(),completion:lease.Token,kind:"complete",key:lease.StorageKey,size:lease.Intent.Request().Bytes},nil}
func(s *archiveProcessorLeaseStub)CloseComplete(context.Context,ArchiveWriterTicket,objectstore.PendingObject)error{s.closes++;return nil}

func(s *archiveProcessorLeaseStub)RecoverComplete(context.Context,ArchiveCompletionLease)(string,error){return s.receipt,nil}

func TestArchiveCrashRecoveryNeverRepeatsComplete(t *testing.T){
 tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"crash",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"bundle.zip",Bytes:22,SHA256:strings.Repeat("a",64)});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"observed","ambiguous","sha-mismatch"}{t.Run(mode,func(t *testing.T){
  leases:=&archiveProcessorLeaseStub{lease:ArchiveCompletionLease{Tenant:tid,Upload:intent.ID(),Token:uuid.New(),Actor:who.UserID,Session:who.SessionID,Intent:intent,StorageKey:intent.ObjectKey(),MultipartID:"multipart",Parts:[]objectstore.UploadedPart{{PartNumber:1,ETag:"etag"}}}}
  objects:=&archiveProcessorObjectsStub{corrupt:mode=="sha-mismatch"};if mode=="ambiguous"{objects.observeErr=objectstore.ErrUnavailable}
  ok,err:=(ArchiveCompletionProcessor{Leases:leases,Writers:leases,Objects:objects,ObserveOnly:true}).ProcessOne(context.Background())
  if objects.complete!=0||objects.recover!=1{t.Fatal("recovery performed physical Complete")}
  switch mode{case "observed":if err!=nil||!ok||leases.closes!=1||leases.finish!=1||objects.verify!=1{t.Fatal("recovery not finalized",err)}
  case "ambiguous":if err==nil||ok||leases.closes!=0||leases.finish!=0||leases.fail!=0||leases.release!=1||objects.verify!=0{t.Fatal("ambiguous writer closed",err)}
  case "sha-mismatch":if err!=nil||!ok||leases.closes!=1||leases.fail!=1||leases.finish!=0{t.Fatal("SHA failure not fenced",err)}}
 })}
}


func TestArchiveRecoveryRejectsMalformedClosedReceiptWithoutStorageIO(t *testing.T){
 tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"receipt-validation",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"bundle.zip",Bytes:22,SHA256:strings.Repeat("a",64)})
 if err!=nil{t.Fatal(err)}
 for _,receipt:=range []string{"null","control\tversion","control\nversion",string([]byte{0xff}),strings.Repeat("x",1025)}{t.Run(fmt.Sprintf("receipt-%x",[]byte(receipt)[:min(len(receipt),12)]),func(t *testing.T){
  leases:=&archiveProcessorLeaseStub{lease:ArchiveCompletionLease{Tenant:tid,Upload:intent.ID(),Token:uuid.New(),Actor:who.UserID,Session:who.SessionID,Intent:intent,StorageKey:intent.ObjectKey(),MultipartID:"multipart",Parts:[]objectstore.UploadedPart{{PartNumber:1,ETag:"etag"}}},receipt:receipt}
  objects:=&archiveProcessorObjectsStub{}
  ok,err:=(ArchiveCompletionProcessor{Leases:leases,Writers:leases,Objects:objects,ObserveOnly:true}).ProcessOne(context.Background())
  if ok||!errors.Is(err,objectstore.ErrIntegrity){t.Fatalf("malformed receipt accepted: %v",err)}
  if objects.complete!=0||objects.recover!=0||objects.verify!=0||leases.starts!=0||leases.closes!=0||leases.finish!=0||leases.fail!=0||leases.release!=1{t.Fatal("malformed receipt caused IO or unsafe writer transition")}
 })}
}


func TestArchiveCompletionCancellationAfterVerificationNeverPublishes(t *testing.T){
 tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"cancelled-verification",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"bundle.zip",Bytes:22,SHA256:strings.Repeat("a",64)})
 if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 leases:=&archiveProcessorLeaseStub{lease:ArchiveCompletionLease{Tenant:tid,Upload:intent.ID(),Token:uuid.New(),Actor:who.UserID,Session:who.SessionID,Intent:intent,StorageKey:intent.ObjectKey(),MultipartID:"multipart",Parts:[]objectstore.UploadedPart{{PartNumber:1,ETag:"etag"}}}}
 objects:=&archiveProcessorObjectsStub{onVerify:cancel}
 ok,err:=(ArchiveCompletionProcessor{Leases:leases,Writers:leases,Objects:objects}).ProcessOne(ctx)
 if ok||!errors.Is(err,context.Canceled){t.Fatalf("cancellation lost: %v",err)}
 if leases.closes!=1||leases.finish!=0||leases.fail!=0||leases.release!=1{t.Fatal("physical receipt lost or cancelled archive published")}
}

func TestArchiveCompletionUsesClaimedGenerationKey(t *testing.T){
 tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"generation-complete",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"bundle.zip",Bytes:22,SHA256:strings.Repeat("a",64)});if err!=nil{t.Fatal(err)}
 key,_:=objectstore.InterchangeGenerationKey(tid,intent.ID(),uuid.New())
 leases:=&archiveProcessorLeaseStub{lease:ArchiveCompletionLease{Tenant:tid,Upload:intent.ID(),Token:uuid.New(),Actor:who.UserID,Session:who.SessionID,Intent:intent,StorageKey:key,MultipartID:"multipart",Parts:[]objectstore.UploadedPart{{PartNumber:1,ETag:"etag"}}}}
 objects:=&archiveProcessorObjectsStub{}
 ok,err:=(ArchiveCompletionProcessor{Leases:leases,Writers:leases,Objects:objects}).ProcessOne(context.Background())
 if err!=nil||!ok||leases.finish!=1||objects.complete!=1||objects.verify!=1{t.Fatal("generation completion failed",ok,err,leases,objects)}
}


func TestArchiveCompletionAdapterPanicRetainsReceiptAndReleasesLease(t *testing.T){
 tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"adapter-panic",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"bundle.zip",Bytes:22,SHA256:strings.Repeat("a",64)})
 if err!=nil{t.Fatal(err)}
 leases:=&archiveProcessorLeaseStub{lease:ArchiveCompletionLease{Tenant:tid,Upload:intent.ID(),Token:uuid.New(),Actor:who.UserID,Session:who.SessionID,Intent:intent,StorageKey:intent.ObjectKey(),MultipartID:"multipart",Parts:[]objectstore.UploadedPart{{PartNumber:1,ETag:"etag"}}}}
 objects:=&archiveProcessorObjectsStub{onVerify:func(){panic("adapter failed")}}
 func(){
  defer func(){if got:=recover();got!="adapter failed"{t.Fatalf("adapter panic changed: %v",got)}}()
  _,_=(ArchiveCompletionProcessor{Leases:leases,Writers:leases,Objects:objects}).ProcessOne(context.Background())
  t.Fatal("adapter panic swallowed")
 }()
 if leases.closes!=1||leases.finish!=0||leases.fail!=0||leases.release!=1{t.Fatal("panic lost physical receipt or published archive")}
}
