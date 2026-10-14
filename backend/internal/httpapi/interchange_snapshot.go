package httpapi
import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/interchange"
 "photoeditor/backend/internal/v2input"
)
type InterchangeSnapshotPreparer interface{
 Prepare(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,[]byte)(interchange.ExportSnapshotReceipt,error)
}
func serveInterchangeSnapshot(w http.ResponseWriter,r *http.Request,who auth.Identity,tid,pid uuid.UUID,service InterchangeSnapshotPreparer){
 key:=r.Header.Get("Idempotency-Key")
 if !idempotency.ValidKey(key){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 r.Body=http.MaxBytesReader(w,r.Body,1<<20)
 var raw json.RawMessage
 if err:=decodeJSON(r,&raw);err!=nil{writeError(w,r,err);return}
 body,err:=canonical.Bytes(raw)
 if err!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 compiled,err:=interchange.CompileExportSnapshotRequest(r.Context(),tid,pid,who,key,body)
 if err!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 if service==nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 receipt,err:=service.Prepare(r.Context(),who,tid,pid,key,append([]byte(nil),body...))
 if err!=nil{if errors.Is(err,v2input.ErrInvalid){err=domain.NewError("VALIDATION_FAILED",422)};writeError(w,r,err);return}
 if r.Context().Err()!=nil{return}
 if err:=compiled.ValidateReceipt(receipt);err!=nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 writeJSON(w,http.StatusCreated,receipt)
}
