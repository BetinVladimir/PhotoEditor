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
 "photoeditor/backend/internal/v2draft"
)
type InterchangeReviewDecider interface{DecideInterchangeReview(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,[]byte)(idempotency.Response,error)}
type InterchangeReviewDecideService struct{Service interchange.Service;Bucket string;Objects artifactjson.Objects;Drafts v2draft.Service}
func(s InterchangeReviewDecideService)DecideInterchangeReview(ctx context.Context,who auth.Identity,tid,id uuid.UUID,key string,raw []byte)(idempotency.Response,error){return s.Service.DecideImport(ctx,who,tid,id,key,raw,s.Bucket,s.Objects,s.Drafts)}
func serveDecideInterchangeReview(w http.ResponseWriter,r *http.Request,who auth.Identity,tid uuid.UUID,closer InterchangeReviewDecider){
 id,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil||id==uuid.Nil||!idempotency.ValidKey(r.Header.Get("Idempotency-Key")){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 r.Body=http.MaxBytesReader(w,r.Body,65536)
 var request interchange.ImportReviewRequest
 if err:=decodeJSON(r,&request);err!=nil{writeError(w,r,err);return}
 raw,err:=canonical.Marshal(request);if err!=nil{writeError(w,r,err);return}
 if closer==nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 response,err:=closer.DecideInterchangeReview(r.Context(),who,tid,id,r.Header.Get("Idempotency-Key"),raw);if err!=nil{writeError(w,r,err);return}
 var status struct{Workflow uuid.UUID `json:"workflow_id"`;Revision string `json:"revision"`;Remaining *int `json:"remaining"`;Child json.RawMessage `json:"child"`;Batch uuid.UUID `json:"batch_id"`}
 decoder:=json.NewDecoder(bytes.NewReader(response.Body));decoder.DisallowUnknownFields();revision,_:=strconv.ParseInt(request.ExpectedRevision,10,64)
 intent,err:=interchange.FreezeImportReviewIntent(r.Context(),tid,id,who,r.Header.Get("Idempotency-Key"),"decide",raw)
 if err!=nil{writeError(w,r,err);return}
 if response.Status!=200||response.ResourceID==nil||*response.ResourceID!=intent.OwnerID()||len(response.Body)>65536||decoder.Decode(&status)!=nil||decoder.Decode(new(any))!=io.EOF||status.Workflow!=id||status.Batch!=intent.OwnerID()||status.Revision!=strconv.FormatInt(revision+1,10)||status.Remaining==nil||*status.Remaining<0||*status.Remaining>5000||string(status.Child)!="null"{writeError(w,r,domain.NewError("INTERCHANGE_DECISION_INVALID_RECEIPT",503));return}
 if response.Replayed{w.Header().Set("Idempotency-Replayed","true")};writeJSON(w,http.StatusOK,status)
}
