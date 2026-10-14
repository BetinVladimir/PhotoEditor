package httpapi
import (
 "context"
 "errors"
 "net/http"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/interchange"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type InterchangeArchiveBeginner interface{
 Begin(context.Context,auth.Identity,uuid.UUID,string,interchange.ArchiveUploadRequest)(interchange.ArchiveUploadIntent,interchange.ArchiveUploadReservation,error)
}
type interchangeArchiveBeginBody struct{Filename string `json:"filename"`;Bytes int64 `json:"bytes"`;SHA256 string `json:"sha256"`}
type interchangeArchiveBeginReceipt struct{
 UploadID uuid.UUID `json:"upload_id"`
 State string `json:"state"`
 PartBytes int64 `json:"part_bytes"`
 PartsTotal int `json:"parts_total"`
 RequestSHA string `json:"request_sha256"`
}
func serveInterchangeArchiveBegin(w http.ResponseWriter,r *http.Request,who auth.Identity,tid,pid uuid.UUID,service InterchangeArchiveBeginner){
 key:=r.Header.Get("Idempotency-Key");if !idempotency.ValidKey(key){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 r.Body=http.MaxBytesReader(w,r.Body,16384);var body interchangeArchiveBeginBody
 if err:=decodeJSON(r,&body);err!=nil{writeError(w,r,err);return}
 request:=interchange.ArchiveUploadRequest{ProjectID:pid,Filename:body.Filename,Bytes:body.Bytes,SHA256:body.SHA256}
 expected,err:=interchange.FreezeArchiveUpload(r.Context(),tid,who,key,request)
 if err!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 if service==nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 intent,reservation,err:=service.Begin(r.Context(),who,tid,key,request)
 if err!=nil{
  switch{case errors.Is(err,v2input.ErrInvalid):err=domain.NewError("VALIDATION_FAILED",422)
  case errors.Is(err,artifactjson.ErrConflict):err=domain.NewError("IDEMPOTENCY_CONFLICT",409)
  case errors.Is(err,artifactjson.ErrQuota):err=domain.NewError("QUOTA_EXCEEDED",409)
  case errors.Is(err,artifactjson.ErrStale):err=domain.NewError("UPLOAD_EXPIRED",410)}
  writeError(w,r,err);return
 }
 if r.Context().Err()!=nil{return}
 if intent.ID()!=expected.ID()||intent.RequestSHA()!=expected.RequestSHA()||intent.ObjectKey()!=expected.ObjectKey()||reservation.Token==uuid.Nil{
  writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return
 }
 switch reservation.State{case "uploading","completing","verified":default:writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 writeJSON(w,http.StatusCreated,interchangeArchiveBeginReceipt{UploadID:intent.ID(),State:reservation.State,PartBytes:objectstore.PartBytes,PartsTotal:expected.PartsTotal(),RequestSHA:expected.RequestSHA()})
}
