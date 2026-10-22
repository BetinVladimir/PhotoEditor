package httpapi
import("testing";"net/http/httptest";"github.com/google/uuid";"photoeditor/backend/internal/auth")
func TestCollectionCullingGroupsMountedAndAuthenticated(t *testing.T){
 handler:=(&Server{Auth:&auth.Service{Pepper:[]byte("collection-culling")}}).Routes()
 r:=httptest.NewRequest("GET","/api/v1/tenants/"+uuid.NewString()+"/collections/"+uuid.NewString()+"/culling-groups",nil);w:=httptest.NewRecorder();handler.ServeHTTP(w,r);if w.Code!=401{t.Fatal("missing mount or unauthenticated projection",w.Code,w.Body.String())}
}
