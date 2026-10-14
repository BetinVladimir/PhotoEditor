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
type ArchiveUploadBeginRepository interface{
 Reserve(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent)(ArchiveUploadReservation,error)
 Bind(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID,string)error
}
type ArchiveUploadBeginObjects interface{
 BeginInterchangeMultipart(context.Context,uuid.UUID,uuid.UUID)(string,error)
 AbortInterchangeMultipart(context.Context,uuid.UUID,uuid.UUID,string)error
}
type ArchiveOpenBeginInspector interface{
 Inspect(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(ArchiveBeginInspection,error)
}
type ArchiveCrossKeyStatusReader interface{
 RequiresReconciliation(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(bool,error)
}
type ArchiveUploadBeginService struct{Repository ArchiveUploadBeginRepository;Objects ArchiveUploadBeginObjects;Writers ArchiveBeginWriters;Inspector ArchiveOpenBeginInspector;Generations ArchiveBeginGenerationWriters;CrossKey ArchiveCrossKeyStatusReader}
func(s ArchiveUploadBeginService)Begin(ctx context.Context,who auth.Identity,tid uuid.UUID,key string,request ArchiveUploadRequest)(ArchiveUploadIntent,ArchiveUploadReservation,error){
 var zero ArchiveUploadIntent;var empty ArchiveUploadReservation
 if s.Repository==nil||s.Objects==nil||s.Writers==nil{return zero,empty,v2input.ErrInvalid}
 intent,err:=FreezeArchiveUpload(ctx,tid,who,key,request);if err!=nil{return zero,empty,err}
 reservation,err:=s.Repository.Reserve(ctx,who,tid,intent);if err!=nil{return zero,empty,err}
 if err:=ctx.Err();err!=nil{return zero,empty,err}
 if reservation.Token==uuid.Nil{return zero,empty,objectstore.ErrIntegrity}
 switch reservation.State{
 case "reserved":if reservation.MultipartID!=""{return zero,empty,objectstore.ErrIntegrity}
 case "uploading","completing","verified":if !objectstore.ValidInterchangeMultipartID(reservation.MultipartID){return zero,empty,objectstore.ErrIntegrity};return intent,reservation,nil
 default:return zero,empty,objectstore.ErrIntegrity
 }
 recovered,err:=s.Writers.RecoverBegin(ctx,who,tid,intent,reservation.Token);if err!=nil{return zero,empty,err}
 if err:=ctx.Err();err!=nil{return zero,empty,err}
 if recovered!=""{
  if !objectstore.ValidInterchangeMultipartID(recovered){return zero,empty,objectstore.ErrIntegrity}
  bindErr:=s.Repository.Bind(ctx,who,tid,intent,reservation.Token,recovered)
  if bindErr==nil{
   if err:=ctx.Err();err!=nil{return zero,empty,err}
   return intent,ArchiveUploadReservation{Token:reservation.Token,State:"uploading",MultipartID:recovered},nil
  }
  // A lost Bind reply may have committed. Read durable authority before
  // reporting a recovered session; never create or abort another multipart.
  recoverCtx,done:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
  actual,recoverErr:=s.Repository.Reserve(recoverCtx,who,tid,intent);done()
  if recoverErr!=nil{return zero,empty,bindErr}
  if actual.Token!=reservation.Token{return zero,empty,objectstore.ErrIntegrity}
  if actual.State=="reserved"{return zero,empty,bindErr}
  if actual.State!="uploading"&&actual.State!="completing"&&actual.State!="verified"{return zero,empty,artifactjson.ErrStale}
  if !objectstore.ValidInterchangeMultipartID(actual.MultipartID)||actual.MultipartID!=recovered{return zero,empty,objectstore.ErrIntegrity}
  if err:=ctx.Err();err!=nil{return zero,empty,err}
  return intent,actual,nil
 }
 if s.Generations!=nil{return s.beginGeneration(ctx,who,tid,intent,reservation)}
 ticket,err:=s.Writers.StartBegin(ctx,who,tid,intent,reservation.Token)
 if err!=nil{
  if errors.Is(err,artifactjson.ErrStale)&&ctx.Err()==nil{
   if s.CrossKey!=nil{
    required,checkErr:=s.CrossKey.RequiresReconciliation(ctx,who,tid,intent,reservation.Token);if checkErr!=nil{return zero,empty,checkErr}
    if required{return zero,empty,domain.NewError("ARCHIVE_BEGIN_RECONCILIATION_REQUIRED",409)}
   }
   if s.Inspector!=nil{
    inspection,inspectErr:=s.Inspector.Inspect(ctx,who,tid,intent,reservation.Token)
    if inspectErr==nil&&inspection.WriterToken!=uuid.Nil{
     return zero,empty,domain.NewError("ARCHIVE_BEGIN_RECONCILIATION_REQUIRED",409)
    }
   }
  }
  return zero,empty,err
 }
 if ticket.tenant!=tid||ticket.upload!=intent.ID()||ticket.token==uuid.Nil||ticket.kind!="begin"||ticket.actor.UserID!=who.UserID||ticket.actor.SessionID!=who.SessionID{return zero,empty,objectstore.ErrIntegrity}
 if err:=ctx.Err();err!=nil{return zero,empty,err}
 work,stop:=context.WithTimeout(ctx,30*time.Second)
 multipart,err:=s.Objects.BeginInterchangeMultipart(work,tid,intent.ID());stop();if err!=nil{return zero,empty,err}
 if !objectstore.ValidInterchangeMultipartID(multipart){return zero,empty,objectstore.ErrIntegrity}
 // Only a concrete successful S3 receipt closes the writer. Errors and lost
 // replies retain an open ticket for reconciliation and block destructive GC.
 closeCtx,closeDone:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
 closeErr:=s.Writers.CloseBegin(closeCtx,ticket,multipart);closeDone();if closeErr!=nil{return zero,empty,closeErr}
 // The durable receipt permits recovery on retry even if the caller stopped.
 if err:=ctx.Err();err!=nil{return zero,empty,err}
 bindErr:=s.Repository.Bind(ctx,who,tid,intent,reservation.Token,multipart)
 if bindErr==nil{
  if err:=ctx.Err();err!=nil{return zero,empty,err}
  return intent,ArchiveUploadReservation{Token:reservation.Token,State:"uploading",MultipartID:multipart},nil
 }
 // A lost commit response can mean this session WON. Read committed authority
 // before aborting anything. Failure to reconcile leaves an orphan for cleanup.
 recoverCtx,stop:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
 actual,recoverErr:=s.Repository.Reserve(recoverCtx,who,tid,intent);stop()
 if recoverErr!=nil{return zero,empty,bindErr}
 if actual.Token!=reservation.Token{return zero,empty,objectstore.ErrIntegrity}
 if actual.State=="reserved"{return zero,empty,bindErr}
 if actual.State!="uploading"&&actual.State!="completing"&&actual.State!="verified"{return zero,empty,artifactjson.ErrStale}
 if !objectstore.ValidInterchangeMultipartID(actual.MultipartID){return zero,empty,objectstore.ErrIntegrity}
 if actual.MultipartID!=multipart{
  cleanup,done:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
  abortErr:=s.Objects.AbortInterchangeMultipart(cleanup,tid,intent.ID(),multipart);done()
  if abortErr!=nil{return zero,empty,abortErr}
 }
 if err:=ctx.Err();err!=nil{return zero,empty,err}
 return intent,actual,nil
}
