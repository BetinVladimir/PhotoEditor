package httpapi

import (
 "net/http"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
)

// Idempotency is the immutable (review,decision_hash) tuple. Retrying after a
// lost response returns the same SQL-derived version; no new render is started.
func (s *Server) serveRelightReviewApply(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","private, no-store")
 who,tid,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
 id,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
 var request struct{DecisionHash string `json:"decision_hash"`}
 if err=decodeJSON(r,&request);err!=nil{writeError(w,r,err);return}
 if _,err=domain.ParseDigest(request.DecisionHash);err!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 if s.Reviews==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
 out,err:=s.Reviews.ApplyRelightSelected(r.Context(),who,tid,id,request.DecisionHash)
 if err!=nil{writeError(w,r,err);return}
 if r.Context().Err()!=nil{return}
 writeJSON(w,http.StatusOK,out)
}

func (s *Server) serveRelightCollectionTargets(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","private, no-store")
 who,tid,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
 id,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
 hash:=r.URL.Query().Get("decision_hash")
 if _,err=domain.ParseDigest(hash);err!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 var after *uuid.UUID
 if raw:=r.URL.Query().Get("after");raw!=""{value,e:=uuid.Parse(raw);if e!=nil||value==uuid.Nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return};after=&value}
 if s.Reviews==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
 out,err:=s.Reviews.RelightCollectionTargets(r.Context(),who,tid,id,hash,after)
 if err!=nil{writeError(w,r,err);return}
 if r.Context().Err()!=nil{return}
 writeJSON(w,http.StatusOK,out)
}
