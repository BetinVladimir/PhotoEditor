package httpapi
import("context";"encoding/json";"net/http";"github.com/go-chi/chi/v5";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/domain")
type interchangeJobReader interface{Get(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(json.RawMessage,error)}
func serveInterchangeJob(w http.ResponseWriter,r *http.Request,who auth.Identity,tid uuid.UUID,reader interchangeJobReader){
 id,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil||id==uuid.Nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 if reader==nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 resource,err:=reader.Get(r.Context(),who,tid,id);if err!=nil{writeError(w,r,err);return}
 writeJSON(w,http.StatusOK,resource)
}

type interchangeInspector interface{Inspect(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(json.RawMessage,error)}
func serveInterchangeInspection(w http.ResponseWriter,r *http.Request,who auth.Identity,tid uuid.UUID,reader interchangeInspector){
 id,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil||id==uuid.Nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 if reader==nil{writeError(w,r,domain.NewError("INTERCHANGE_UNAVAILABLE",503));return}
 result,err:=reader.Inspect(r.Context(),who,tid,id);if err!=nil{writeError(w,r,err);return}
 writeJSON(w,http.StatusOK,result)
}
