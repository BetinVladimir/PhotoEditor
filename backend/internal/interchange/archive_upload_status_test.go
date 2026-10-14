package interchange
import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)
type archiveStatusGenerationReader struct{ snapshot *ArchiveBeginGenerationSnapshot;err error }
func(r archiveStatusGenerationReader)Read(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(*ArchiveBeginGenerationSnapshot,error){return r.snapshot,r.err}
type archiveStatusOnlyReader struct{status *ArchiveBeginGenerationStatus;session uuid.UUID;calls int}
func(r *archiveStatusOnlyReader)ReadStatus(_ context.Context,who auth.Identity,_ uuid.UUID,_ ArchiveUploadIntent,_ uuid.UUID)(*ArchiveBeginGenerationStatus,error){r.calls++;r.session=who.SessionID;return r.status,nil}
type archiveStatusOpenBeginReader struct{open bool;calls int}
func(r *archiveStatusOpenBeginReader)Open(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID)(bool,error){r.calls++;return r.open,nil}
func statusGenerationSnapshot(t *testing.T,tid uuid.UUID,who auth.Identity,intent ArchiveUploadIntent,reservation uuid.UUID,state string)*ArchiveBeginGenerationSnapshot{
 t.Helper();generation:=uuid.New();key,err:=objectstore.InterchangeGenerationKey(tid,intent.ID(),generation);if err!=nil{t.Fatal(err)}
 multipart:="";if state!="creating"{multipart="multipart"}
 return &ArchiveBeginGenerationSnapshot{Ticket:ArchiveBeginGenerationTicket{tenant:tid,upload:intent.ID(),generation:generation,writer:uuid.New(),reservation:reservation,actor:who,requestSHA:intent.RequestSHA(),key:key},State:state,MultipartID:multipart}
}
func TestArchiveStatusClassifiesBeginGenerationWithoutStorageIO(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 p,err:=FreezeArchiveUpload(context.Background(),tid,who,"status-generation",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:p,Reservation:ArchiveUploadReservation{Token:uuid.New(),State:"reserved"},ExpiresAt:time.Now().Add(time.Hour)}}
 for _,tc:=range []struct{generation,state,want string}{{"creating","reserved","reconciliation_required"},{"created","reserved","receipt_pending_bind"},{"bound","uploading","resolved"},{"aborting","aborting","none"}}{
  stub.record.Reservation.State=tc.state
  if tc.state=="uploading"{stub.record.Reservation.MultipartID="multipart"}else{stub.record.Reservation.MultipartID=""}
  if tc.state=="aborting"{stub.record.ErrorCode="UPLOAD_ABORTED"}else{stub.record.ErrorCode=""}
  service:=ArchiveUploadActions{Repository:stub,BeginGenerations:archiveStatusGenerationReader{snapshot:statusGenerationSnapshot(t,tid,who,p,stub.record.Reservation.Token,tc.generation)}}
  status,err:=service.Status(context.Background(),who,tid,pid,p.ID(),p.RequestSHA());if err!=nil||status.BeginRecovery!=tc.want{t.Fatal(tc,status,err)}
 }
}
func TestArchiveStatusUsesCurrentSessionWithoutCreatorTicket(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();actor:=uuid.New()
 creator:=auth.Identity{UserID:actor,SessionID:uuid.New()}
 current:=auth.Identity{UserID:actor,SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,creator,"new-session-status",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:intent,Reservation:ArchiveUploadReservation{Token:uuid.New(),State:"reserved"},ExpiresAt:time.Now().Add(time.Hour)}}
 readonly:=&archiveStatusOnlyReader{status:&ArchiveBeginGenerationStatus{State:"created",MultipartID:"multipart"}}
 service:=ArchiveUploadActions{Repository:stub,BeginGenerations:archiveStatusGenerationReader{err:errors.New("creator ticket must not be read")},GenerationStatus:readonly}
 status,err:=service.Status(context.Background(),current,tid,pid,intent.ID(),intent.RequestSHA())
 if err!=nil||status.BeginRecovery!="receipt_pending_bind"||readonly.calls!=1||readonly.session!=current.SessionID{t.Fatal("current-session status lost saved Create receipt",status,err,readonly)}
}
func TestArchiveStatusRefusesAbortingGenerationWithReservedParent(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"aborting-reserved",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 reservation:=uuid.New();stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:intent,Reservation:ArchiveUploadReservation{Token:reservation,State:"reserved"},ExpiresAt:time.Now().Add(time.Hour)}}
 service:=ArchiveUploadActions{Repository:stub,BeginGenerations:archiveStatusGenerationReader{snapshot:statusGenerationSnapshot(t,tid,who,intent,reservation,"aborting")}}
 status,err:=service.Status(context.Background(),who,tid,pid,intent.ID(),intent.RequestSHA())
 if !errors.Is(err,objectstore.ErrIntegrity)||status.BeginRecovery!=""{t.Fatal("impossible cleanup state appeared actionable",status,err)}
}
func TestArchiveStatusClassifiesAbortDuringBeginGeneration(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"abort-during-begin",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 reservation:=uuid.New()
 for _,tc:=range []struct{generation,multipart,want string}{
  {"creating","","reconciliation_required"},
  {"created","","none"},
  {"bound","multipart","none"},
  {"aborting","multipart","none"},
 }{t.Run(tc.generation,func(t *testing.T){
  stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:intent,Reservation:ArchiveUploadReservation{Token:reservation,State:"aborting",MultipartID:tc.multipart},ErrorCode:"UPLOAD_ABORTED",ExpiresAt:time.Now().Add(time.Hour)}}
  snapshot:=statusGenerationSnapshot(t,tid,who,intent,reservation,tc.generation)
  service:=ArchiveUploadActions{Repository:stub,BeginGenerations:archiveStatusGenerationReader{snapshot:snapshot}}
  status,err:=service.Status(context.Background(),who,tid,pid,intent.ID(),intent.RequestSHA())
  if err!=nil||status.BeginRecovery!=tc.want{t.Fatal(tc,status,err)}
 })}
}
func TestArchiveStatusRefusesAbortWithSubstitutedGenerationMultipart(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"abort-substituted",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 reservation:=uuid.New()
 for _,generation:=range []string{"bound","aborting"}{t.Run(generation,func(t *testing.T){
  stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:intent,Reservation:ArchiveUploadReservation{Token:reservation,State:"aborting",MultipartID:"foreign"},ErrorCode:"UPLOAD_ABORTED",ExpiresAt:time.Now().Add(time.Hour)}}
  service:=ArchiveUploadActions{Repository:stub,BeginGenerations:archiveStatusGenerationReader{snapshot:statusGenerationSnapshot(t,tid,who,intent,reservation,generation)}}
  status,err:=service.Status(context.Background(),who,tid,pid,intent.ID(),intent.RequestSHA())
  if !errors.Is(err,objectstore.ErrIntegrity)||status.BeginRecovery!=""{t.Fatal("foreign multipart passed abort status",status,err)}
 })}
}
func TestArchiveAttemptStatusReadsOnlyMatchingSavedIntent(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 request:=ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"saved-key",request);if err!=nil{t.Fatal(err)}
 stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:intent,Reservation:ArchiveUploadReservation{Token:uuid.New(),State:"reserved"},ExpiresAt:time.Now().Add(time.Hour)}}
 service:=ArchiveUploadActions{Repository:stub,BeginGenerations:archiveStatusGenerationReader{snapshot:statusGenerationSnapshot(t,tid,who,intent,stub.record.Reservation.Token,"creating")}}
 status,err:=service.AttemptStatus(context.Background(),who,tid,"saved-key",request);if err!=nil||status.UploadID!=intent.ID()||status.BeginRecovery!="reconciliation_required"{t.Fatal(status,err)}
 if _,err:=service.AttemptStatus(context.Background(),who,tid,"different-key",request);err==nil{t.Fatal("different attempt key read stored status")}
 other:=request;other.SHA256=rawSHA([]byte("other"))
 if _,err:=service.AttemptStatus(context.Background(),who,tid,"saved-key",other);err==nil{t.Fatal("different request read stored status")}
}
func TestArchiveStatusRetainsLegacyOpenBeginReconciliation(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 p,err:=FreezeArchiveUpload(context.Background(),tid,who,"legacy-open",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:p,Reservation:ArchiveUploadReservation{Token:uuid.New(),State:"reserved"},ExpiresAt:time.Now().Add(time.Hour)}}
 open:=&archiveStatusOpenBeginReader{open:true}
 service:=ArchiveUploadActions{Repository:stub,BeginGenerations:archiveStatusGenerationReader{},OpenBegin:open}
 status,err:=service.Status(context.Background(),who,tid,pid,p.ID(),p.RequestSHA());if err!=nil||status.BeginRecovery!="reconciliation_required"||open.calls!=1{t.Fatal(status,err,open.calls)}
 service.BeginGenerations=archiveStatusGenerationReader{snapshot:statusGenerationSnapshot(t,tid,who,p,stub.record.Reservation.Token,"created")}
 status,err=service.Status(context.Background(),who,tid,pid,p.ID(),p.RequestSHA());if err!=nil||status.BeginRecovery!="receipt_pending_bind"||open.calls!=1{t.Fatal(status,err,open.calls)}
}
func TestArchiveStatusRefusesSubstitutedBeginGeneration(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"generation-scope",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 reservation:=uuid.New();stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:intent,Reservation:ArchiveUploadReservation{Token:reservation,State:"reserved"},ExpiresAt:time.Now().Add(time.Hour)}}
 for _,mode:=range []string{"tenant","reservation","actor","request","multipart"}{t.Run(mode,func(t *testing.T){
  snapshot:=statusGenerationSnapshot(t,tid,who,intent,reservation,"creating")
  switch mode{case "tenant":snapshot.Ticket.tenant=uuid.New();case "reservation":snapshot.Ticket.reservation=uuid.New();case "actor":snapshot.Ticket.actor.UserID=uuid.New();case "request":snapshot.Ticket.requestSHA=rawSHA([]byte("other"));case "multipart":snapshot.MultipartID="foreign"}
  service:=ArchiveUploadActions{Repository:stub,BeginGenerations:archiveStatusGenerationReader{snapshot:snapshot}}
  status,err:=service.Status(context.Background(),who,tid,pid,intent.ID(),intent.RequestSHA())
  if !errors.Is(err,objectstore.ErrIntegrity)||status.BeginRecovery!=""{t.Fatal("foreign generation influenced status",mode,status,err)}
 })}
}
func TestArchiveStatusBoundGenerationRequiresExactMultipart(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeArchiveUpload(context.Background(),tid,who,"bound-generation",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 reservation:=uuid.New();stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:intent,Reservation:ArchiveUploadReservation{Token:reservation,State:"uploading",MultipartID:"other-multipart"},ExpiresAt:time.Now().Add(time.Hour)}}
 snapshot:=statusGenerationSnapshot(t,tid,who,intent,reservation,"bound")
 service:=ArchiveUploadActions{Repository:stub,BeginGenerations:archiveStatusGenerationReader{snapshot:snapshot}}
 for _,state:=range []string{"uploading","completing","verified","failed"}{
  stub.record.Reservation.State=state
  status,err:=service.Status(context.Background(),who,tid,pid,intent.ID(),intent.RequestSHA())
  if !errors.Is(err,objectstore.ErrIntegrity)||status.BeginRecovery!=""{t.Fatal("substituted bound multipart resolved",state,status,err)}
 }
 stub.record.Reservation.State="uploading";stub.record.Reservation.MultipartID=snapshot.MultipartID
 status,err:=service.Status(context.Background(),who,tid,pid,intent.ID(),intent.RequestSHA())
 if err!=nil||status.BeginRecovery!="resolved"{t.Fatal("exact bound multipart refused",status,err)}
}
func TestArchiveStatusRequiresIndependentReadyEvidence(t *testing.T){
 tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 p,err:=FreezeArchiveUpload(context.Background(),tid,who,"status",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 stub:=&archiveActionStub{record:ArchiveUploadRecord{Intent:p,Reservation:ArchiveUploadReservation{Token:uuid.New(),State:"verified",MultipartID:"multipart"},ExpiresAt:time.Now().Add(time.Hour)}}
 service:=ArchiveUploadActions{Repository:stub}
 status,err:=service.Status(context.Background(),who,tid,pid,p.ID(),p.RequestSHA());if err!=nil||status.Ready||status.Artifact!=nil{t.Fatal("ledger state alone granted readiness",status,err)}
 stub.record.Ready=true
 status,err=service.Status(context.Background(),who,tid,pid,p.ID(),p.RequestSHA());if err!=nil||status.Artifact==nil||status.Artifact.ArtifactID!=p.ArtifactID()||status.Artifact.SHA256!=p.Request().SHA256{t.Fatal(status,err)}
 stub.record.Reservation.State="completing"
 if _,err:=service.Status(context.Background(),who,tid,pid,p.ID(),p.RequestSHA());err==nil{t.Fatal("ready while completing")}
}

func TestArchiveStatusRejectsMalformedRepositoryEnvelope(t *testing.T){
 for _,mode:=range []string{"zero-expiry","unknown-state","unknown-error","ready-uploading","active-with-error","aborting-without-error","aborted-without-error","reserved-multipart","uploading-empty","uploading-invalid","verified-empty","failed-invalid"}{t.Run(mode,func(t *testing.T){
  tid,pid:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
  p,err:=FreezeArchiveUpload(context.Background(),tid,who,"status-envelope",ArchiveUploadRequest{ProjectID:pid,Filename:"a.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
  record:=ArchiveUploadRecord{Intent:p,Reservation:ArchiveUploadReservation{Token:uuid.New(),State:"uploading",MultipartID:"multipart"},ExpiresAt:time.Now().Add(time.Hour)}
  switch mode{case "zero-expiry":record.ExpiresAt=time.Time{};case "unknown-state":record.Reservation.State="internal";case "unknown-error":record.ErrorCode="secret storage error";case "ready-uploading":record.Ready=true;case "active-with-error":record.ErrorCode="UPLOAD_ABORTED";case "aborting-without-error":record.Reservation.State="aborting";case "aborted-without-error":record.Reservation.State="aborted";case "reserved-multipart":record.Reservation.State="reserved";case "uploading-empty":record.Reservation.MultipartID="";case "uploading-invalid":record.Reservation.MultipartID="bad\nID";case "verified-empty":record.Reservation.State="verified";record.Reservation.MultipartID="";case "failed-invalid":record.Reservation.State="failed";record.Reservation.MultipartID="bad\nID"}
  service:=ArchiveUploadActions{Repository:&archiveActionStub{record:record}}
  status,err:=service.Status(context.Background(),who,tid,pid,p.ID(),p.RequestSHA());if err==nil||status.Artifact!=nil||status.Ready{t.Fatal("malformed repository status escaped",status,err)}
 })}
}
