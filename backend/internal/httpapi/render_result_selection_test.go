package httpapi

import (
 "net/http/httptest"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

func TestRenderResultSelectionRouteRequiresAuthentication(t *testing.T){
 server:=&Server{Auth:&auth.Service{Pepper:[]byte("render-result-selection")}}
 url:="/api/v1/tenants/"+uuid.NewString()+"/reviews/"+uuid.NewString()+"/render-result-selection"
 response:=httptest.NewRecorder()
 server.Routes().ServeHTTP(response,httptest.NewRequest("GET",url,nil))
 if response.Code!=401||response.Header().Get("Cache-Control")!="private, no-store"{
  t.Fatal("render result selection exposed without session",response.Code,response.Header())
 }
}
