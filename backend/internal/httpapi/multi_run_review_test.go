package httpapi
import (
 "testing"
 "net/http/httptest"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestMultiRunReviewRequiresAuthenticationAndNoStore(t *testing.T){
 s:=&Server{Auth:&auth.Service{Pepper:[]byte("multi-history")}}
 w:=httptest.NewRecorder();r:=httptest.NewRequest("GET","/api/v1/tenants/"+uuid.NewString()+"/runs/"+uuid.NewString()+"/multi-review",nil)
 s.Routes().ServeHTTP(w,r)
 if w.Code!=401||w.Header().Get("Cache-Control")!="no-store"{t.Fatal(w.Code,w.Header())}
}
