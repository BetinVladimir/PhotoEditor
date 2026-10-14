package httpapi
import (
 "context"
 "net/http"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/interchange"
)
type InterchangeArchiveStatusReader interface{
 Status(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,string)(interchange.ArchiveUploadStatus,error)
}
func serveInterchangeArchiveStatus(w http.ResponseWriter,r *http.Request,who auth.Identity,tid,pid,upload uuid.UUID,service InterchangeArchiveStatusReader){
 values,ok:=r.URL.Query()["request_sha256"]
 if !ok||len(values)!=1||!archiveRequestSHA.MatchString(values[0]){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 if service==nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 status,err:=service.Status(r.Context(),who,tid,pid,upload,values[0]);if err!=nil{writeError(w,r,archiveActionError(err));return}
 if r.Context().Err()!=nil{return}
 if status.BeginRecovery==""{status.BeginRecovery="none"}
 valid:=validInterchangeArchiveStatus(status,upload,values[0])
 if !valid{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,status)
}
func validInterchangeArchiveStatus(status interchange.ArchiveUploadStatus,upload uuid.UUID,requestSHA string)bool{
 valid:=status.UploadID==upload&&status.RequestSHA==requestSHA&&!status.ExpiresAt.IsZero()
 switch status.State{case "reserved","uploading","completing","verified","aborting","aborted","failed":default:valid=false}
 switch status.ErrorCode{case "","ARCHIVE_INTEGRITY","UPLOAD_ABORTED","UPLOAD_EXPIRED":default:valid=false}
 switch status.BeginRecovery{case "none","reconciliation_required","receipt_pending_bind","resolved":default:valid=false}
 if status.BeginRecovery=="reconciliation_required"&&status.State!="reserved"{valid=false}
 if status.BeginRecovery=="receipt_pending_bind"&&status.State!="reserved"{valid=false}
 if status.BeginRecovery=="resolved"&&status.State=="reserved"{valid=false}
 if (status.State=="aborting"||status.State=="aborted")!=(status.ErrorCode!=""){valid=false}
 if status.Ready{valid=valid&&status.State=="verified"&&status.Artifact!=nil&&status.Artifact.ArtifactID==uuid.NewSHA1(upload,[]byte("archive:artifact:1"))&&archiveRequestSHA.MatchString(status.Artifact.SHA256)}else{valid=valid&&status.Artifact==nil}
 return valid
}
