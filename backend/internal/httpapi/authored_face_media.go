package httpapi

import (
 "io"
 "net/http"
 "strconv"
 "github.com/go-chi/chi/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2render"
)

// Explicit mount seam: final server composition must supply the actual API-role
// pool/object reader. No unsigned derived URL or arbitrary provider callback.
func(s *Server) authoredFaceMediaRoutes(router chi.Router,service v2render.AuthoredFaceMediaService){
 router.Get("/api/v1/tenants/{t}/authored-renders/{parent}/phases/{ordinal}/face-selection/{candidate}/media",func(w http.ResponseWriter,r *http.Request){
  who,tid,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
  parent,ordinal,sha,err:=authoredFaceSelectionScope(r);if err!=nil{writeError(w,r,err);return}
  var requestBody []byte;if r.Body!=nil{requestBody,err=io.ReadAll(io.LimitReader(r.Body,1))};if err!=nil||len(requestBody)!=0||r.URL.RawQuery!=""||r.Header.Get("Range")!=""{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  value,err:=service.Read(r.Context(),who,tid,parent,ordinal,sha);if err!=nil{writeError(w,r,authoredFaceSelectionHTTPError(err));return}
  body:=value.Bytes() // owned copy before last authority query, never callback storage
  if err=service.Recheck(r.Context(),value);err!=nil{writeError(w,r,authoredFaceSelectionHTTPError(err));return}
  if r.Context().Err()!=nil{return}
  w.Header().Set("Cache-Control","private, no-store");w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Content-Disposition","inline");w.Header().Set("Content-Type",value.ContentType());w.Header().Set("Content-Length",strconv.Itoa(len(body)))
  w.WriteHeader(http.StatusOK);_,_=w.Write(body)
 })
}
