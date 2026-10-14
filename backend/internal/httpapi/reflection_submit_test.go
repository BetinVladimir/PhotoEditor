package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestReflectionMutationRoutesRequireAuthenticatedSession(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("reflection-route")}}
	for _, suffix := range []string{"reflection-mask-pairs", "reflection-preparations", "reflection-intents"} {
		path := "/api/v1/tenants/" + uuid.NewString() + "/projects/" +
			uuid.NewString() + "/" + suffix
		response := httptest.NewRecorder()
		request := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		server.Routes().ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatalf("%s accepted unauthenticated mutation: %d", suffix, response.Code)
		}
	}
}
