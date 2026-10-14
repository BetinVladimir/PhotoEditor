package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
)

func TestOpenEyesPreparationRequiresAuthenticatedSession(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("open-eyes-route")}}
	for _, suffix := range []string{"open-eyes-preparations", "open-eyes-intents", "open-eyes-source-candidates", "open-eyes-donor-candidates", "open-eyes-mask-pairs"} {
		path := "/api/v1/tenants/" + uuid.NewString() + "/projects/" + uuid.NewString() + "/" + suffix
		response := httptest.NewRecorder()
		request := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		server.Routes().ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatalf("unauthenticated %s accepted: %d", suffix, response.Code)
		}
	}
}
