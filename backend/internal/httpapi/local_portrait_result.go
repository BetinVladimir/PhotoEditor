package httpapi

import (
 "net/http"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
)

func(s *Server) serveLocalPortraitResult(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","private, no-store")
 who,tid,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
 id,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
 if s.Reviews==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
 out,err:=s.Reviews.ReadLocalPortraitResult(r.Context(),who,tid,id);if err!=nil{writeError(w,r,err);return}
 if r.Context().Err()==nil{writeJSON(w,http.StatusOK,out)}
}
func(s *Server) serveLocalPortraitResultApply(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","private, no-store")
 who,tid,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
 id,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
 var request struct{ResultID uuid.UUID `json:"result_id"`;CandidateSHA string `json:"candidate_sha"`}
 if err=decodeJSON(r,&request);err!=nil{writeError(w,r,err);return}
 if request.ResultID==uuid.Nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 if _,err=domain.ParseDigest(request.CandidateSHA);err!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 if s.Reviews==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
 out,err:=s.Reviews.ApplyLocalPortraitResult(r.Context(),who,tid,id,request.ResultID,request.CandidateSHA)
 if err!=nil{writeError(w,r,err);return};if r.Context().Err()==nil{writeJSON(w,http.StatusOK,out)}
}
