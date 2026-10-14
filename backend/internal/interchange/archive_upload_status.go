package interchange
import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type ArchiveUploadStatus struct{
 UploadID uuid.UUID `json:"upload_id"`
 State string `json:"state"`
 RequestSHA string `json:"request_sha256"`
 ExpiresAt time.Time `json:"expires_at"`
 ErrorCode string `json:"error_code,omitempty"`
 Ready bool `json:"ready"`
 BeginRecovery string `json:"begin_recovery"`
 Artifact *v2input.Ref `json:"artifact,omitempty"`
}
func(s ArchiveUploadActions)Status(ctx context.Context,who auth.Identity,tid,pid,upload uuid.UUID,requestSHA string)(ArchiveUploadStatus,error){
 record,err:=s.load(ctx,who,tid,pid,upload,requestSHA);if err!=nil{return ArchiveUploadStatus{},err}
 switch record.ErrorCode{case "","ARCHIVE_INTEGRITY","UPLOAD_ABORTED","UPLOAD_EXPIRED":default:return ArchiveUploadStatus{},objectstore.ErrIntegrity}
 if record.ExpiresAt.IsZero(){return ArchiveUploadStatus{},objectstore.ErrIntegrity}
 switch record.Reservation.State{case "reserved","uploading","completing","verified","aborting","aborted","failed":default:return ArchiveUploadStatus{},objectstore.ErrIntegrity}
 switch record.Reservation.State{
 case "reserved":if record.Reservation.MultipartID!=""{return ArchiveUploadStatus{},objectstore.ErrIntegrity}
 case "uploading","completing","verified":if !objectstore.ValidInterchangeMultipartID(record.Reservation.MultipartID){return ArchiveUploadStatus{},objectstore.ErrIntegrity}
 default:if record.Reservation.MultipartID!=""&&!objectstore.ValidInterchangeMultipartID(record.Reservation.MultipartID){return ArchiveUploadStatus{},objectstore.ErrIntegrity}
 }
 if (record.Reservation.State=="aborting"||record.Reservation.State=="aborted")!=(record.ErrorCode!=""){return ArchiveUploadStatus{},objectstore.ErrIntegrity}
 status:=ArchiveUploadStatus{UploadID:upload,State:record.Reservation.State,RequestSHA:record.Intent.RequestSHA(),ExpiresAt:record.ExpiresAt,ErrorCode:record.ErrorCode,Ready:record.Ready,BeginRecovery:"none"}
 if s.GenerationStatus!=nil||s.BeginGenerations!=nil{
  var generation *ArchiveBeginGenerationStatus
  if s.GenerationStatus!=nil{
   generation,err=s.GenerationStatus.ReadStatus(ctx,who,tid,record.Intent,record.Reservation.Token)
  }else{
   var snapshot *ArchiveBeginGenerationSnapshot
   snapshot,err=s.BeginGenerations.Read(ctx,who,tid,record.Intent,record.Reservation.Token)
   if err==nil&&snapshot!=nil{
    if !validBeginGenerationSnapshot(*snapshot,who,tid,record.Intent,record.Reservation.Token){return ArchiveUploadStatus{},objectstore.ErrIntegrity}
    generation=&ArchiveBeginGenerationStatus{State:snapshot.State,MultipartID:snapshot.MultipartID}
   }
  }
  if err!=nil{return ArchiveUploadStatus{},err}
  if err:=ctx.Err();err!=nil{return ArchiveUploadStatus{},err}
  if generation!=nil{
   switch generation.State{
   case "creating":if generation.MultipartID!=""{return ArchiveUploadStatus{},objectstore.ErrIntegrity}
   case "created","bound","aborting":if !objectstore.ValidInterchangeMultipartID(generation.MultipartID){return ArchiveUploadStatus{},objectstore.ErrIntegrity}
   default:return ArchiveUploadStatus{},objectstore.ErrIntegrity
   }
   switch generation.State{
   case "creating":
    if (status.State!="reserved"&&status.State!="aborting")||record.Reservation.MultipartID!=""{return ArchiveUploadStatus{},objectstore.ErrIntegrity}
    status.BeginRecovery="reconciliation_required"
   case "created":
    if (status.State!="reserved"&&status.State!="aborting")||record.Reservation.MultipartID!=""{return ArchiveUploadStatus{},objectstore.ErrIntegrity}
    if status.State=="reserved"{status.BeginRecovery="receipt_pending_bind"}
   case "bound":
    if status.State=="aborting"{
     if record.Reservation.MultipartID!=generation.MultipartID{return ArchiveUploadStatus{},objectstore.ErrIntegrity}
     break
    }
    if (status.State!="uploading"&&status.State!="completing"&&status.State!="verified")||record.Reservation.MultipartID!=generation.MultipartID{return ArchiveUploadStatus{},objectstore.ErrIntegrity}
    status.BeginRecovery="resolved"
   case "aborting":
    if status.State!="aborting"||(record.Reservation.MultipartID!=""&&record.Reservation.MultipartID!=generation.MultipartID){return ArchiveUploadStatus{},objectstore.ErrIntegrity}
    status.BeginRecovery="none"
   default:return ArchiveUploadStatus{},objectstore.ErrIntegrity
   }
  }
 }
 if status.BeginRecovery=="none"&&record.Reservation.State=="reserved"&&s.OpenBegin!=nil{
  open,err:=s.OpenBegin.Open(ctx,who,tid,pid,upload,record.Reservation.Token);if err!=nil{return ArchiveUploadStatus{},err}
  if open{status.BeginRecovery="reconciliation_required"}
 }
 if record.Ready{
  if record.Reservation.State!="verified"{return ArchiveUploadStatus{},objectstore.ErrIntegrity}
  status.Artifact=&v2input.Ref{ArtifactID:record.Intent.ArtifactID(),SHA256:record.Intent.Request().SHA256}
 }
 return status,ctx.Err()
}

// AttemptStatus never reserves a new upload. It derives the immutable upload
// identity from the caller's saved key and verifies that the stored request is
// exactly the same before exposing the scoped status.
func(s ArchiveUploadActions)AttemptStatus(ctx context.Context,who auth.Identity,tid uuid.UUID,key string,request ArchiveUploadRequest)(ArchiveUploadStatus,error){
 intent,err:=FreezeArchiveUpload(ctx,tid,who,key,request);if err!=nil{return ArchiveUploadStatus{},err}
 record,err:=s.load(ctx,who,tid,request.ProjectID,intent.ID(),intent.RequestSHA());if err!=nil{return ArchiveUploadStatus{},err}
 if record.Intent.key!=key||string(record.Intent.RequestBody())!=string(intent.RequestBody())||record.Intent.RequestSHA()!=intent.RequestSHA(){return ArchiveUploadStatus{},artifactjson.ErrConflict}
 return s.Status(ctx,who,tid,request.ProjectID,intent.ID(),intent.RequestSHA())
}
