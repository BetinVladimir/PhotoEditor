package httpapi
import (
 "context"
 "errors"
 "net/http"
 "regexp"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type InterchangeArchiveActions interface{
 Abort(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,string)(bool,error)
 Parts(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,string,[]int)([]objectstore.PartURL,error)
 Complete(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,string,[]objectstore.UploadedPart)error
}
var archiveRequestSHA=regexp.MustCompile(`^[0-9a-f]{64}$`)
func archiveActionError(err error)error{
 switch{case errors.Is(err,v2input.ErrInvalid):return domain.NewError("VALIDATION_FAILED",422)
 case errors.Is(err,artifactjson.ErrConflict):return domain.NewError("IDEMPOTENCY_CONFLICT",409)
 case errors.Is(err,artifactjson.ErrStale):return domain.NewError("UPLOAD_EXPIRED",410)
 case errors.Is(err,objectstore.ErrIntegrity):return domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 return err
}
func serveInterchangeArchiveParts(w http.ResponseWriter,r *http.Request,who auth.Identity,tid,pid,upload uuid.UUID,service InterchangeArchiveActions){
 r.Body=http.MaxBytesReader(w,r.Body,16384)
 var body struct{RequestSHA string `json:"request_sha256"`;Parts []int `json:"parts"`}
 if err:=decodeJSON(r,&body);err!=nil{writeError(w,r,err);return}
 if !archiveRequestSHA.MatchString(body.RequestSHA)||len(body.Parts)<1||len(body.Parts)>100{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 seen:=map[int]bool{};for _,n:=range body.Parts{if n<1||n>objectstore.MaxUploadParts||seen[n]{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return};seen[n]=true}
 if service==nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 urls,err:=service.Parts(r.Context(),who,tid,pid,upload,body.RequestSHA,append([]int(nil),body.Parts...))
 if err!=nil{writeError(w,r,archiveActionError(err));return};if r.Context().Err()!=nil{return}
 if len(urls)!=len(seen){writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 now:=time.Now().UTC()
 for _,url:=range urls{if !seen[url.PartNumber]||!objectstore.ValidInterchangePartURL(url,now,time.Time{}){writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return};delete(seen,url.PartNumber)}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,struct{Parts []objectstore.PartURL `json:"parts"`}{urls})
}
func serveInterchangeArchiveComplete(w http.ResponseWriter,r *http.Request,who auth.Identity,tid,pid,upload uuid.UUID,service InterchangeArchiveActions){
 r.Body=http.MaxBytesReader(w,r.Body,1<<20)
 var body struct{RequestSHA string `json:"request_sha256"`;Parts []objectstore.UploadedPart `json:"parts"`}
 if err:=decodeJSON(r,&body);err!=nil{writeError(w,r,err);return}
 if !archiveRequestSHA.MatchString(body.RequestSHA)||len(body.Parts)<1||len(body.Parts)>objectstore.MaxUploadParts{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 if service==nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 if err:=service.Complete(r.Context(),who,tid,pid,upload,body.RequestSHA,append([]objectstore.UploadedPart(nil),body.Parts...));err!=nil{writeError(w,r,archiveActionError(err));return}
 if r.Context().Err()!=nil{return}
 // Accepted means queued or replayed, never a claim that SHA verification passed.
 writeJSON(w,http.StatusAccepted,struct{UploadID uuid.UUID `json:"upload_id"`;Accepted bool `json:"accepted"`}{upload,true})
}

func serveInterchangeArchiveAbort(w http.ResponseWriter,r *http.Request,who auth.Identity,tid,pid,upload uuid.UUID,service InterchangeArchiveActions){
 r.Body=http.MaxBytesReader(w,r.Body,1024)
 var body struct{RequestSHA string `json:"request_sha256"`}
 if err:=decodeJSON(r,&body);err!=nil{writeError(w,r,err);return}
 if !archiveRequestSHA.MatchString(body.RequestSHA){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 if service==nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 pending,err:=service.Abort(r.Context(),who,tid,pid,upload,body.RequestSHA);if err!=nil{writeError(w,r,archiveActionError(err));return}
 if r.Context().Err()!=nil{return}
 writeJSON(w,http.StatusAccepted,struct{UploadID uuid.UUID `json:"upload_id"`;Accepted bool `json:"accepted"`;CleanupPending bool `json:"cleanup_pending"`}{upload,true,pending})
}
