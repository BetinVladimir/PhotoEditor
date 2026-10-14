package interchange

import (
 "context"
 "errors"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type ArchiveBeginGenerationWriters interface{
 Read(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(*ArchiveBeginGenerationSnapshot,error)
 Start(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(ArchiveBeginGenerationTicket,error)
 Close(context.Context,ArchiveBeginGenerationTicket,string)error
 FailBeforeDispatch(context.Context,ArchiveBeginGenerationTicket)error
 Bind(context.Context,ArchiveBeginGenerationTicket,string)error
}
type ArchiveBeginGenerationObjects interface{
 BeginInterchangeGenerationMultipart(context.Context,uuid.UUID,uuid.UUID,uuid.UUID)(string,error)
}

func(s ArchiveUploadBeginService)beginGeneration(ctx context.Context,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent,reservation ArchiveUploadReservation)(ArchiveUploadIntent,ArchiveUploadReservation,error){
 var zero ArchiveUploadIntent;var empty ArchiveUploadReservation
 if s.Generations==nil||reservation.State!="reserved"||reservation.Token==uuid.Nil{return zero,empty,v2input.ErrInvalid}
 objects,ok:=s.Objects.(ArchiveBeginGenerationObjects);if !ok{return zero,empty,v2input.ErrInvalid}
 snapshot,err:=s.Generations.Read(ctx,who,tid,intent,reservation.Token);if err!=nil{return zero,empty,err}
 if snapshot!=nil{
  if !validBeginGenerationSnapshot(*snapshot,who,tid,intent,reservation.Token){return zero,empty,objectstore.ErrIntegrity}
  switch snapshot.State{
  case "creating","aborting":return zero,empty,domain.NewError("ARCHIVE_BEGIN_RECONCILIATION_REQUIRED",409)
  case "created","bound":return s.bindGenerationReceipt(ctx,who,tid,intent,reservation,*snapshot)
  default:return zero,empty,objectstore.ErrIntegrity
  }
 }
 ticket,err:=s.Generations.Start(ctx,who,tid,intent,reservation.Token)
 if err!=nil{
  if errors.Is(err,artifactjson.ErrStale)&&ctx.Err()==nil{
   current,readErr:=s.Generations.Read(ctx,who,tid,intent,reservation.Token)
   if readErr==nil&&current!=nil&&validBeginGenerationSnapshot(*current,who,tid,intent,reservation.Token){
    if current.State=="created"||current.State=="bound"{return s.bindGenerationReceipt(ctx,who,tid,intent,reservation,*current)}
    return zero,empty,domain.NewError("ARCHIVE_BEGIN_RECONCILIATION_REQUIRED",409)
   }
   if s.CrossKey!=nil{
    required,checkErr:=s.CrossKey.RequiresReconciliation(ctx,who,tid,intent,reservation.Token);if checkErr!=nil{return zero,empty,checkErr}
    if required{return zero,empty,domain.NewError("ARCHIVE_BEGIN_RECONCILIATION_REQUIRED",409)}
   }
   if s.Inspector!=nil{
    inspection,inspectErr:=s.Inspector.Inspect(ctx,who,tid,intent,reservation.Token)
    if inspectErr==nil&&inspection.WriterToken!=uuid.Nil{return zero,empty,domain.NewError("ARCHIVE_BEGIN_RECONCILIATION_REQUIRED",409)}
   }
  }
  return zero,empty,err
 }
 if !validBeginGenerationTicket(ticket,who,tid,intent,reservation.Token){return zero,empty,objectstore.ErrIntegrity}
 if err:=ctx.Err();err!=nil{
  failCtx,done:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
  failErr:=s.Generations.FailBeforeDispatch(failCtx,ticket);done()
  if failErr!=nil{return zero,empty,failErr}
  return zero,empty,err
 }
 work,stop:=context.WithTimeout(ctx,30*time.Second)
 multipart,err:=objects.BeginInterchangeGenerationMultipart(work,tid,intent.ID(),ticket.generation);stop()
 if err!=nil{
  if errors.Is(err,objectstore.ErrInterchangeCreateNotDispatched){
   failCtx,done:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
   failErr:=s.Generations.FailBeforeDispatch(failCtx,ticket);done()
   if failErr!=nil{return zero,empty,failErr}
   return zero,empty,err
  }
  // Once the POST reached the guarded transport, even a timeout can hide a
  // successful Create. Tell the caller to use read-only attempt status; the
  // writer and quota stay fenced until a terminal receipt is proven.
  return zero,empty,domain.NewError("ARCHIVE_BEGIN_RECONCILIATION_REQUIRED",409)
 }
 if !objectstore.ValidInterchangeMultipartID(multipart){return zero,empty,objectstore.ErrIntegrity}
 closeCtx,closeDone:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
 closeErr:=s.Generations.Close(closeCtx,ticket,multipart);closeDone()
 if closeErr!=nil{
  // Close may have committed despite a lost SQL reply. A repeat with the same
  // S3 receipt is idempotent; never start another Create to recover it.
  recoverCtx,recoverDone:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
  saved,readErr:=s.Generations.Read(recoverCtx,who,tid,intent,reservation.Token)
  if readErr==nil&&saved!=nil&&validBeginGenerationSnapshot(*saved,who,tid,intent,reservation.Token){
   if (saved.State=="created"||saved.State=="bound")&&saved.MultipartID==multipart{
    recoverDone()
    if err:=ctx.Err();err!=nil{return zero,empty,err}
    return s.bindGenerationReceipt(ctx,who,tid,intent,reservation,*saved)
   }
   if saved.State!="creating"||saved.MultipartID!=""{recoverDone();return zero,empty,objectstore.ErrIntegrity}
  }
  recoverDone()
  retryCtx,retryDone:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
  retryErr:=s.Generations.Close(retryCtx,ticket,multipart);retryDone()
  if retryErr!=nil{return zero,empty,closeErr}
 }
 if err:=ctx.Err();err!=nil{return zero,empty,err}
 return s.bindGenerationReceipt(ctx,who,tid,intent,reservation,ArchiveBeginGenerationSnapshot{Ticket:ticket,State:"created",MultipartID:multipart})
}

func validBeginGenerationTicket(ticket ArchiveBeginGenerationTicket,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent,reservation uuid.UUID)bool{
 return ticket.valid()&&ticket.tenant==tid&&ticket.upload==intent.ID()&&ticket.reservation==reservation&&ticket.actor.UserID==who.UserID&&ticket.actor.SessionID==who.SessionID&&ticket.requestSHA==intent.RequestSHA()
}
func validBeginGenerationSnapshot(snapshot ArchiveBeginGenerationSnapshot,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent,reservation uuid.UUID)bool{
 if !validBeginGenerationTicket(snapshot.Ticket,who,tid,intent,reservation){return false}
 switch snapshot.State{
 case "creating":return snapshot.MultipartID==""
 case "created","bound","aborting":return objectstore.ValidInterchangeMultipartID(snapshot.MultipartID)
 default:return false
 }
}
func(s ArchiveUploadBeginService)bindGenerationReceipt(ctx context.Context,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent,reservation ArchiveUploadReservation,snapshot ArchiveBeginGenerationSnapshot)(ArchiveUploadIntent,ArchiveUploadReservation,error){
 var zero ArchiveUploadIntent;var empty ArchiveUploadReservation
 if !validBeginGenerationSnapshot(snapshot,who,tid,intent,reservation.Token)||snapshot.State=="creating"||snapshot.State=="aborting"{return zero,empty,objectstore.ErrIntegrity}
 bindErr:=s.Generations.Bind(ctx,snapshot.Ticket,snapshot.MultipartID)
 if bindErr==nil{
  if err:=ctx.Err();err!=nil{return zero,empty,err}
  return intent,ArchiveUploadReservation{Token:reservation.Token,State:"uploading",MultipartID:snapshot.MultipartID},nil
 }
 // A lost Bind reply can have committed. Read the authoritative reservation;
 // never abort this generation or start a replacement while its ID is known.
 recoverCtx,done:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
 actual,err:=s.Repository.Reserve(recoverCtx,who,tid,intent);done()
 if err!=nil{return zero,empty,bindErr}
 if actual.Token!=reservation.Token{return zero,empty,objectstore.ErrIntegrity}
 if actual.State=="reserved"{return zero,empty,bindErr}
 if actual.State!="uploading"&&actual.State!="completing"&&actual.State!="verified"{return zero,empty,artifactjson.ErrStale}
 if actual.MultipartID!=snapshot.MultipartID{return zero,empty,objectstore.ErrIntegrity}
 if err:=ctx.Err();err!=nil{return zero,empty,err}
 return intent,actual,nil
}
