package httpapi
import("testing";"net/http/httptest";"github.com/google/uuid";"photoeditor/backend/internal/auth")
func TestAppliedPreviewRouteRequiresAuthentication(t *testing.T){
 server:=&Server{Auth:&auth.Service{Pepper:[]byte("target-preview")}}
 response:=httptest.NewRecorder();server.Routes().ServeHTTP(response,httptest.NewRequest("GET","/api/v1/tenants/"+uuid.NewString()+"/semantic-targets/"+uuid.NewString()+"/preview",nil))
 if response.Code!=401{t.Fatalf("status=%d",response.Code)}
}
