package httpapi

import (
 "context"
 "net/http"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/interchange"
)

type interchangeArchiveAttemptStatusReader interface{
 AttemptStatus(context.Context,auth.Identity,uuid.UUID,string,interchange.ArchiveUploadRequest)(interchange.ArchiveUploadStatus,error)
}

// A read-only POST carries the original closed Begin body, including the
// filename, without putting its identity in URLs or creating a reservation.
func serveInterchangeArchiveAttemptStatus(w http.ResponseWriter,r *http.Request,who auth.Identity,tid,pid uuid.UUID,reader InterchangeArchiveStatusReader){
 key:=r.Header.Get("Idempotency-Key")
 if !idempotency.ValidKey(key){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 r.Body=http.MaxBytesReader(w,r.Body,16384)
 var body interchangeArchiveBeginBody
 if err:=decodeJSON(r,&body);err!=nil{writeError(w,r,err);return}
 request:=interchange.ArchiveUploadRequest{ProjectID:pid,Filename:body.Filename,Bytes:body.Bytes,SHA256:body.SHA256}
 if _,err:=interchange.FreezeArchiveUpload(r.Context(),tid,who,key,request);err!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 service,ok:=reader.(interchangeArchiveAttemptStatusReader);if !ok{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 status,err:=service.AttemptStatus(r.Context(),who,tid,key,request);if err!=nil{writeError(w,r,archiveActionError(err));return}
 if r.Context().Err()!=nil{return}
 expected,err:=interchange.FreezeArchiveUpload(r.Context(),tid,who,key,request)
 if err!=nil||!validInterchangeArchiveStatus(status,expected.ID(),expected.RequestSHA()){writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,status)
}
