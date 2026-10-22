package httpapi

import (
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestLocalPortraitResultRoutesRequireAuthenticatedSession(t *testing.T){
 for _,method:=range []string{"GET","POST"}{
  t.Run(method,func(t *testing.T){
   handler:=(&Server{Auth:&auth.Service{Pepper:[]byte("local-portrait-result")}}).Routes()
   suffix:="local-portrait-result";if method=="POST"{suffix="local-portrait-apply"}
   request:=httptest.NewRequest(method,"/api/v1/tenants/"+uuid.NewString()+"/v2-workflows/"+uuid.NewString()+"/"+suffix,strings.NewReader(`{"result_id":"`+uuid.NewString()+`","candidate_sha":"`+strings.Repeat("a",64)+`"}`))
   request.Header.Set("Content-Type","application/json")
   response:=httptest.NewRecorder();handler.ServeHTTP(response,request)
   if response.Code!=401{t.Fatal("unauthenticated result authority allowed",response.Code)}
  })
 }
}
