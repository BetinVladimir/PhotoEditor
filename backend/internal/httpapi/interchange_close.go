package httpapi
import(
 "bytes"
 "context"
 "encoding/json"
 "io"
 "net/http"
 "strconv"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/interchange"
)
type InterchangeReviewCloser interface{CloseInterchangeReview(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,[]byte)(idempotency.Response,error)}
type InterchangeReviewCloseService struct{Service interchange.Service;Bucket string;Objects artifactjson.Objects}
func(s InterchangeReviewCloseService)CloseInterchangeReview(ctx context.Context,who auth.Identity,tid,id uuid.UUID,key string,raw []byte)(idempotency.Response,error){return s.Service.CloseImportReview(ctx,who,tid,id,key,raw,s.Bucket,s.Objects)}
func serveCloseInterchangeReview(w http.ResponseWriter,r *http.Request,who auth.Identity,tid uuid.UUID,closer InterchangeReviewCloser){
 id,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil||id==uuid.Nil||!idempotency.ValidKey(r.Header.Get("Idempotency-Key")){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 r.Body=http.MaxBytesReader(w,r.Body,65536)
 var request interchange.ImportReviewCloseRequest
 if err:=decodeJSON(r,&request);err!=nil{writeError(w,r,err);return}
 raw,err:=canonical.Marshal(request);if err!=nil{writeError(w,r,err);return}
 if closer==nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 response,err:=closer.CloseInterchangeReview(r.Context(),who,tid,id,r.Header.Get("Idempotency-Key"),raw);if err!=nil{writeError(w,r,err);return}
 if r.Context().Err()!=nil{return}
 var status struct{Run uuid.UUID `json:"run_id"`;Workflow uuid.UUID `json:"workflow_id"`;Revision string `json:"revision"`;State string `json:"state"`;Outputs []json.RawMessage `json:"outputs"`;Error *string `json:"error_code"`}
 decoder:=json.NewDecoder(bytes.NewReader(response.Body));decoder.DisallowUnknownFields();revision,_:=strconv.ParseInt(request.ExpectedRevision,10,64)
 if response.Status!=200||response.ResourceID==nil||*response.ResourceID!=id||len(response.Body)>65536||decoder.Decode(&status)!=nil||decoder.Decode(new(any))!=io.EOF||status.Run==uuid.Nil||status.Workflow!=id||status.Revision!=strconv.FormatInt(revision+1,10)||status.State!="completed"||status.Outputs==nil||len(status.Outputs)!=0||status.Error!=nil{writeError(w,r,domain.NewError("INTERCHANGE_CLOSE_INVALID_RECEIPT",503));return}
 if response.Replayed{w.Header().Set("Idempotency-Replayed","true")};writeJSON(w,http.StatusOK,status)
}
