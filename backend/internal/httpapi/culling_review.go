package httpapi

import (
 "net/http"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
)

func(s *Server) serveCullingReviewItems(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","private, no-store")
 who,tid,e:=s.tenantRequest(r,false);if e!=nil{writeError(w,r,e);return};id,e:=routeID(r,"id");if e!=nil{writeError(w,r,e);return}
 query:=r.URL.Query();if len(query)>1||len(query["after"])>1||len(query.Get("after"))>2048{writeError(w,r,domain.NewError("INVALID_CURSOR",400));return};for key:=range query{if key!="after"{writeError(w,r,domain.NewError("INVALID_CURSOR",400));return}}
 if s.Reviews==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
 out,e:=s.Reviews.CullingItems(r.Context(),who,tid,id,query.Get("after"));if e!=nil{writeError(w,r,e);return};if r.Context().Err()!=nil{return};writeJSON(w,http.StatusOK,out)
}
func(s *Server) serveCullingReviewDecision(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","private, no-store")
 who,tid,e:=s.tenantRequest(r,true);if e!=nil{writeError(w,r,e);return};id,e:=routeID(r,"id");if e!=nil{writeError(w,r,e);return}
 var req review.CullingDecision;if e=decodeJSON(r,&req);e!=nil{writeError(w,r,e);return}
 if s.Reviews==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
 out,e:=s.Reviews.DecideCulling(r.Context(),who,tid,id,r.Header.Get("Idempotency-Key"),req);if e!=nil{writeError(w,r,e);return};if r.Context().Err()!=nil{return}
 if out.Replayed{w.Header().Set("Idempotency-Replayed","true")};w.Header().Set("Content-Type","application/json");w.WriteHeader(out.Status);_,_=w.Write(out.Body)
}
