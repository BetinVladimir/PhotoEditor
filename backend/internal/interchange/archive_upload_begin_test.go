package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)
type archiveBeginStub struct{token,writerToken uuid.UUID;mode string;reserves,begins,aborts,starts,closes,binds int;cancel context.CancelFunc;receipt string;sqlRecovered,keepReserved bool}
func(s *archiveBeginStub)Reserve(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent)(ArchiveUploadReservation,error){
 s.reserves++;r:=ArchiveUploadReservation{Token:s.token,State:"reserved"}
 if s.mode=="bad-token"{r.Token=uuid.Nil}
 if s.mode=="bad-state"{r.State="aborted"}
 if s.mode=="bad-replay"{r.State="uploading";r.MultipartID="bad\x00id"}
 if s.mode=="replay"{r.State="uploading";r.MultipartID="winner"}
 if s.sqlRecovered{r.State="uploading";r.MultipartID="saved-multipart"}
 if s.reserves>1&&s.mode!="recovered-unbound"&&!s.keepReserved{if s.mode=="unknown"{return r,errors.New("read failed")};r.State="uploading";r.MultipartID="created";if s.mode=="loser"{r.MultipartID="winner"}}
 if s.mode=="bad-reconcile"&&s.reserves>1{r.MultipartID="bad\tid"}
 return r,nil
}
func(s *archiveBeginStub)Bind(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID,string)error{s.binds++;if s.closes!=1&&s.mode!="closed-receipt"&&s.mode!="sql-recovered"{return errors.New("bind before writer close")};if s.mode=="ok"||s.mode=="closed-receipt"||s.mode=="sql-recovered"{return nil};return errors.New("ambiguous bind")}
func(s *archiveBeginStub)BeginInterchangeMultipart(context.Context,uuid.UUID,uuid.UUID)(string,error){s.begins++;if s.starts!=1{return "",errors.New("S3 before writer ticket")};if s.mode=="bad-created"{return "bad\x00id",nil};if s.mode=="create-unknown"{return "",errors.New("create response lost")};if s.mode=="cancel-created"{s.cancel()};return "created",nil}
func(s *archiveBeginStub)BeginInterchangeGenerationMultipart(context.Context,uuid.UUID,uuid.UUID,uuid.UUID)(string,error){s.begins++;if s.mode=="generation-unknown"{return "",errors.New("create response lost")};if s.mode=="generation-not-dispatched"{return "",objectstore.ErrInterchangeCreateNotDispatched};if s.mode=="cancel-after-generation-create"&&s.cancel!=nil{s.cancel()};return "created",nil}
func(s *archiveBeginStub)AbortInterchangeMultipart(_ context.Context,_,_ uuid.UUID,id string)error{s.aborts++;if id!="created"{return errors.New("aborted winner")};return nil}
func TestArchiveBeginRecoversCommitAndAbortsOnlyProvenLoser(t *testing.T){
 for _,mode:=range []string{"ok","replay","lost-commit","loser","unknown","closed-receipt"}{t.Run(mode,func(t *testing.T){
  stub:=&archiveBeginStub{token:uuid.New(),mode:mode}
  _,r,err:=(ArchiveUploadBeginService{Repository:stub,Objects:stub,Writers:stub}).Begin(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),"begin",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))})
  if mode=="unknown"{if err==nil||stub.aborts!=0{t.Fatal("ambiguous session aborted",err,stub)};return}
  if err!=nil||r.State!="uploading"{t.Fatal(r,err)}
  wantAbort:=0;if mode=="loser"{wantAbort=1};if stub.aborts!=wantAbort{t.Fatal("wrong abort count",stub)}
  if (mode=="replay"||mode=="closed-receipt")&&stub.begins!=0{t.Fatal("replay created another session")};if mode=="closed-receipt"&&stub.binds!=1{t.Fatal("saved receipt was not bound to SQL",stub)};if stub.begins>0&&(stub.writerToken==uuid.Nil||stub.writerToken==stub.token){t.Fatal("writer and reservation tokens must be distinct",stub)}
 })}
}

func(s *archiveBeginStub)StartBegin(_ context.Context,who auth.Identity,tid uuid.UUID,p ArchiveUploadIntent,_ uuid.UUID)(ArchiveWriterTicket,error){s.starts++;if s.mode=="open-stale"{return ArchiveWriterTicket{},artifactjson.ErrStale};if s.mode=="bad-actor"{who.UserID=uuid.New()};s.writerToken=uuid.New();return ArchiveWriterTicket{tenant:tid,upload:p.ID(),token:s.writerToken,kind:"begin",actor:who},nil}

type archiveOpenBeginInspectionStub struct{calls int;open bool}
func(s *archiveOpenBeginInspectionStub)Inspect(_ context.Context,_ auth.Identity,_ uuid.UUID,_ ArchiveUploadIntent,_ uuid.UUID)(ArchiveBeginInspection,error){s.calls++;if s.open{return ArchiveBeginInspection{WriterToken:uuid.New(),ObservedMultipart:""},nil};return ArchiveBeginInspection{},nil}

func TestArchiveBeginReportsOpenWriterWithoutCreatingSecondMultipart(t *testing.T){
 for _,open:=range []bool{false,true}{
  stub:=&archiveBeginStub{token:uuid.New(),mode:"open-stale"};inspector:=&archiveOpenBeginInspectionStub{open:open}
  _,_,err:=(ArchiveUploadBeginService{Repository:stub,Objects:stub,Writers:stub,Inspector:inspector}).Begin(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),"begin",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))})
  if inspector.calls!=1||stub.begins!=0||stub.closes!=0||stub.binds!=0||stub.aborts!=0{t.Fatal("open writer caused storage mutation",open,stub,inspector)}
  if open&& (err==nil||err.Error()!="ARCHIVE_BEGIN_RECONCILIATION_REQUIRED"){t.Fatal("open writer not classified",err)}
  if !open&&!errors.Is(err,artifactjson.ErrStale){t.Fatal("ordinary stale outcome changed",err)}
 }
}
func(s *archiveBeginStub)CloseBegin(_ context.Context,_ ArchiveWriterTicket,receipt string)error{s.closes++;s.receipt=receipt;if s.mode=="close-unknown"{return errors.New("close commit response lost")};return nil}

func(s *archiveBeginStub)RecoverBegin(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(string,error){if s.mode=="create-unknown"&&s.starts>0{return "",errors.New("open begin requires reconciliation")};if s.receipt!=""{return s.receipt,nil};if s.mode=="bad-recovered"{return "bad\tid",nil};if s.mode=="closed-receipt"||s.mode=="recovered-unbound"{return "saved-multipart",nil};if s.mode=="sql-recovered"{s.sqlRecovered=true;return "saved-multipart",nil};return "",nil}

func TestArchiveBeginAcceptsSQLBoundRecoveredReceiptWithoutCreatingMultipart(t *testing.T){
 stub:=&archiveBeginStub{token:uuid.New(),mode:"sql-recovered"}
 _,reservation,err:=(ArchiveUploadBeginService{Repository:stub,Objects:stub,Writers:stub}).Begin(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),"begin",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))})
 if err!=nil||reservation.State!="uploading"||reservation.MultipartID!="saved-multipart"||stub.binds!=1||stub.begins!=0{t.Fatal("SQL-bound recovered receipt not accepted idempotently",reservation,err,stub)}
}

func TestArchiveBeginDoesNotReportUnboundRecoveredSession(t *testing.T){
 stub:=&archiveBeginStub{token:uuid.New(),mode:"recovered-unbound"}
 _,reservation,err:=(ArchiveUploadBeginService{Repository:stub,Objects:stub,Writers:stub}).Begin(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),"begin",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))})
 if err==nil||reservation.State=="uploading"||stub.binds!=1||stub.reserves!=2||stub.begins!=0||stub.aborts!=0{t.Fatal("unbound saved receipt reported uploading or created another multipart",reservation,err,stub)}
}

func TestArchiveBeginRefusesInvalidDependencyReceipts(t *testing.T){
 for _,mode:=range []string{"bad-token","bad-state","bad-replay","bad-created","bad-recovered","bad-reconcile","bad-actor"}{t.Run(mode,func(t *testing.T){
  stub:=&archiveBeginStub{token:uuid.New(),mode:mode}
  _,_,err:=(ArchiveUploadBeginService{Repository:stub,Objects:stub,Writers:stub}).Begin(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),"begin",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))})
  if err==nil||stub.aborts!=0{t.Fatal("invalid receipt accepted or destructive reconciliation",err,stub)}
  if mode!="bad-created"&&mode!="bad-reconcile"&&stub.begins!=0{t.Fatal("invalid authority reached storage",stub)}
  if mode!="bad-reconcile"&&stub.closes!=0{t.Fatal("invalid receipt closed writer",stub)}
 })}
}

func TestArchiveBeginRetainsUnknownOutcomeAndRecoversSavedReceipt(t *testing.T){
 for _,mode:=range []string{"create-unknown","close-unknown","cancel-created"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  stub:=&archiveBeginStub{token:uuid.New(),mode:mode,cancel:cancel}
  service:=ArchiveUploadBeginService{Repository:stub,Objects:stub,Writers:stub}
  who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid:=uuid.New()
  request:=ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))}
  _,_,err:=service.Begin(ctx,who,tid,"begin",request)
  if err==nil||stub.begins!=1||stub.aborts!=0||stub.binds!=0{t.Fatal("unknown outcome advanced or aborted",mode,err,stub)}
  if mode=="cancel-created"&&!errors.Is(err,context.Canceled){t.Fatal("lost cancellation",err)}
  if mode=="create-unknown"{if stub.closes!=0{t.Fatal("unknown create closed writer")}}else if stub.closes!=1||stub.receipt!="created"{t.Fatal("successful create receipt lost",stub)}
  // Keep SQL reservation unbound so the retry exercises writer recovery,
  // rather than the already-uploading reservation shortcut.
  stub.reserves=0
  _,reservation,retryErr:=service.Begin(context.Background(),who,tid,"begin",request)
  if mode=="create-unknown"{if retryErr==nil{t.Fatal("open writer silently retried")}}else if retryErr!=nil||reservation.MultipartID!="created"||reservation.State!="uploading"{t.Fatal("saved receipt not recovered",reservation,retryErr)}
  if stub.begins!=1||stub.starts!=1||stub.aborts!=0{t.Fatal("retry duplicated storage writer",stub)}
  if mode!="create-unknown"&&stub.binds!=1{t.Fatal("recovered receipt was not bound or reconciled",stub)}
 })}
}
