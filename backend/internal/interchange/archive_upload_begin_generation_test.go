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

type archiveGenerationBeginStub struct{snapshot *ArchiveBeginGenerationSnapshot;ticket,failedTicket ArchiveBeginGenerationTicket;reads,starts,closes,binds,fails int;mode string;cancel context.CancelFunc}
func(s *archiveGenerationBeginStub)Read(_ context.Context,_ auth.Identity,_ uuid.UUID,_ ArchiveUploadIntent,_ uuid.UUID)(*ArchiveBeginGenerationSnapshot,error){s.reads++;return s.snapshot,nil}
func(s *archiveGenerationBeginStub)Start(_ context.Context,who auth.Identity,tid uuid.UUID,p ArchiveUploadIntent,reservation uuid.UUID)(ArchiveBeginGenerationTicket,error){
 if s.mode=="cross-key-refusal"{s.starts++;return ArchiveBeginGenerationTicket{},artifactjson.ErrStale}
 s.starts++;generation:=uuid.New();key,_:=objectstore.InterchangeGenerationKey(tid,p.ID(),generation)
 s.ticket=ArchiveBeginGenerationTicket{tenant:tid,upload:p.ID(),generation:generation,writer:uuid.New(),reservation:reservation,actor:who,requestSHA:p.RequestSHA(),key:key}
 if s.mode=="cancel-after-start"&&s.cancel!=nil{s.cancel()}
 return s.ticket,nil
}
type archiveCrossKeyStatusStub struct{required bool;calls int}
func(s *archiveCrossKeyStatusStub)RequiresReconciliation(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(bool,error){s.calls++;return s.required,nil}
func TestArchiveGenerationBeginClassifiesCrossKeyStartRefusalWithoutCreate(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid:=uuid.New()
 repository:=&archiveBeginStub{token:uuid.New(),mode:"cross-key-refusal"};generation:=&archiveGenerationBeginStub{mode:"cross-key-refusal"};crossKey:=&archiveCrossKeyStatusStub{required:true}
 service:=ArchiveUploadBeginService{Repository:repository,Objects:repository,Writers:repository,Generations:generation,CrossKey:crossKey}
 _,_,err:=service.Begin(context.Background(),who,tid,"saved-key",ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"archive.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))})
 if err==nil||err.Error()!="ARCHIVE_BEGIN_RECONCILIATION_REQUIRED"||crossKey.calls!=1||generation.starts!=1||repository.begins!=0||repository.aborts!=0{t.Fatal("cross-key Start refusal was not safe",err,crossKey,repository)}
}
func(s *archiveGenerationBeginStub)Close(_ context.Context,t ArchiveBeginGenerationTicket,id string)error{
 s.closes++
 if s.mode=="close-unresolved"{return errors.New("close unavailable")}
 if s.mode=="close-transient"&&s.closes==1{return errors.New("close transient")}
 if s.mode=="close-substituted"{s.snapshot=&ArchiveBeginGenerationSnapshot{Ticket:t,State:"created",MultipartID:"other-multipart"};return errors.New("close reply lost")}
 s.snapshot=&ArchiveBeginGenerationSnapshot{Ticket:t,State:"created",MultipartID:id}
 if s.mode=="close-lost"{return errors.New("close reply lost")}
 return nil
}
func(s *archiveGenerationBeginStub)FailBeforeDispatch(_ context.Context,t ArchiveBeginGenerationTicket)error{s.fails++;s.failedTicket=t;s.snapshot=nil;return nil}
func(s *archiveGenerationBeginStub)Bind(_ context.Context,_ ArchiveBeginGenerationTicket,_ string)error{s.binds++;if s.mode=="bind-lost"{return errors.New("bind reply lost")};return nil}

func TestArchiveGenerationBeginRecoveryNeverRepeatsCreate(t *testing.T){
 for _,mode:=range []string{"ok","creating","created","generation-unknown","generation-not-dispatched","cancel-after-start","cancel-after-generation-create","close-lost","close-transient","close-unresolved","close-substituted","bind-lost"}{t.Run(mode,func(t *testing.T){
  who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid:=uuid.New()
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  repository:=&archiveBeginStub{token:uuid.New(),mode:mode,cancel:cancel};generation:=&archiveGenerationBeginStub{mode:mode,cancel:cancel}
  request:=ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))}
  frozen,err:=FreezeArchiveUpload(context.Background(),tid,who,"generation",request);if err!=nil{t.Fatal(err)}
  if mode=="creating"||mode=="created"{
   ticket,_:=generation.Start(context.Background(),who,tid,frozen,repository.token);generation.starts=0
   state:="creating";id:="";if mode=="created"{state="created";id="saved"}
   generation.snapshot=&ArchiveBeginGenerationSnapshot{Ticket:ticket,State:state,MultipartID:id}
  }
  service:=ArchiveUploadBeginService{Repository:repository,Objects:repository,Writers:repository,Generations:generation}
  _,result,err:=service.Begin(ctx,who,tid,"generation",request)
  if mode=="creating"||mode=="generation-unknown"||mode=="generation-not-dispatched"||mode=="cancel-after-start"||mode=="cancel-after-generation-create"||mode=="close-unresolved"||mode=="close-substituted"{
   if err==nil||result.State=="uploading"||generation.binds!=0{t.Fatal("ambiguous generation advanced",mode,result,err)}
  }else if err!=nil||result.State!="uploading"{t.Fatal("generation Begin failed",mode,result,err)}
  if mode=="generation-unknown"&&err.Error()!="ARCHIVE_BEGIN_RECONCILIATION_REQUIRED"{t.Fatal("dispatched Create error was not classified for recovery",err)}
  if mode=="generation-not-dispatched"&&err.Error()=="ARCHIVE_BEGIN_RECONCILIATION_REQUIRED"{t.Fatal("definite no-dispatch was classified as unknown")}
  wantCreate:=1;if mode=="creating"||mode=="created"||mode=="cancel-after-start"{wantCreate=0}
  if repository.begins!=wantCreate||repository.starts!=0||repository.aborts!=0{t.Fatal("generation retry used legacy or repeated Create",mode,repository)}
  if (mode=="generation-not-dispatched"||mode=="cancel-after-start")!=(generation.fails==1){t.Fatal("not-dispatched transition mismatch",mode,generation.fails)}
  if mode=="cancel-after-start"&&generation.failedTicket!=generation.ticket{t.Fatal("cancelled Start did not close its committed generation")}
  if mode=="cancel-after-generation-create"&&(generation.closes!=1||generation.binds!=0||generation.fails!=0){t.Fatal("successful Create receipt was not durably closed after cancellation",generation)}
  if mode=="created"&&generation.binds!=1{t.Fatal("stored receipt was not bound")}
  if mode=="close-lost"&&generation.closes!=1{t.Fatal("lost Close reply repeated committed transition")}
  if mode=="close-transient"&&generation.closes!=2{t.Fatal("transient Close was not retried with same receipt")}
  if mode=="close-unresolved"&&(generation.closes!=2||generation.binds!=0){t.Fatal("unresolved Close advanced",generation)}
  if mode=="close-substituted"&&(!errors.Is(err,objectstore.ErrIntegrity)||generation.closes!=1||generation.binds!=0){t.Fatal("substituted Close receipt advanced",err,generation)}
 })}
}

func TestArchiveGenerationBeginRetriesOnlyAfterProvenNoDispatch(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid:=uuid.New()
 repository:=&archiveBeginStub{token:uuid.New(),mode:"generation-not-dispatched",keepReserved:true}
 generation:=&archiveGenerationBeginStub{mode:"generation-not-dispatched"}
 service:=ArchiveUploadBeginService{Repository:repository,Objects:repository,Writers:repository,Generations:generation}
 request:=ArchiveUploadRequest{ProjectID:uuid.New(),Filename:"retry.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))}
 _,_,err:=service.Begin(context.Background(),who,tid,"same-key",request)
 if !errors.Is(err,objectstore.ErrInterchangeCreateNotDispatched)||generation.fails!=1||repository.begins!=1{t.Fatal("first Create was not proven unsent",err,generation,repository)}
 first:=generation.ticket
 repository.mode="ok";generation.mode="ok"
 _,result,err:=service.Begin(context.Background(),who,tid,"same-key",request)
 if err!=nil||result.State!="uploading"||generation.starts!=2||repository.begins!=2||generation.ticket.generation==first.generation||generation.ticket.key==first.key||generation.binds!=1||repository.aborts!=0{t.Fatal("safe retry did not use a fresh generation",result,err,generation,repository)}
}
