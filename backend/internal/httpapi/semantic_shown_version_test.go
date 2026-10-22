package httpapi

import (
	"github.com/google/uuid"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestShownVersionRouteRequiresAuthentication(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("shown-version")}}
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/semantic-targets/"+uuid.NewString()+"/version", nil))
	if response.Code != 401 {
		t.Fatalf("status=%d", response.Code)
	}
}
