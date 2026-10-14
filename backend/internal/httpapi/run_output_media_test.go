package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/run"
)

func TestRunOutputMediaChecksTokenAndExactVersionBeforeStorage(t *testing.T) {
	now := time.Now()
	pepper := []byte(strings.Repeat("p", 32))
	key := auth.MAC(pepper, "v26-run-output-capability")
	grant := run.OutputGrant{Version: 1, Tenant: uuid.New(), Run: uuid.New(), User: uuid.New(), Session: uuid.New(),
		Artifact: uuid.New(), Role: "working_image", SHA256: strings.Repeat("a", 64), ObjectKeySHA: strings.Repeat("c", 64), ObjectVersion: "v1",
		MediaType: "image/jpeg", Bytes: 1024, ExpiresUnix: now.Add(5 * time.Minute).Unix()}
	token, err := run.SealOutputGrant(key, grant, now)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Auth: &auth.Service{Pepper: pepper}}
	base := "/api/v1/media/run-output?token=" + token + "&versionId="
	for _, tc := range []struct {
		url    string
		status int
	}{
		{base + "v2", 404},
		{base + "v1&versionId=v2", 404},
		{base + "v1&unexpected=1", 404},
		{"/api/v1/media/run-output?versionId=v1", 404},
		{base + "v1", 503},
	} {
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, httptest.NewRequest("GET", tc.url, nil))
		if response.Code != tc.status || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("run-output media boundary", tc.url, response.Code, response.Header())
		}
	}
}
