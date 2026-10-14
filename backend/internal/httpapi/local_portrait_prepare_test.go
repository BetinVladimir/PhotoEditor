package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestLocalPortraitPrepareRequiresAuthenticatedSession(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("local-portrait-route")}}
	for _, suffix := range []string{"local-portrait-mask-pairs", "local-portrait-preparations", "local-portrait-intents"} {
		path := "/api/v1/tenants/" + uuid.NewString() + "/projects/" + uuid.NewString() + "/" + suffix
		response := httptest.NewRecorder()
		request := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		server.Routes().ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatalf("unauthenticated %s accepted: %d", suffix, response.Code)
		}
	}
}
