package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"photoeditor/backend/internal/auth"
	"testing"

	"github.com/go-chi/chi/v5"
)

// Walk the actual router so loop-based registrations count too. A declared
// endpoint contract alone is not evidence that the server exposes the route.
func TestCaptureClientRoutesRegistered(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("route-fixture")}}
	routes, ok := server.Routes().(chi.Routes)
	if !ok {
		t.Fatal("server does not expose router tree")
	}
	registered := map[string]bool{}
	if err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../packages/contracts/v2-endpoints.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Endpoints []struct{ Method, Path, Handler string }
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{"UpdateSession": false, "ListSessions": false, "GetSession": false, "GetShotList": false, "ReplaceShotList": false, "ListPersons": false, "ListAlerts": false, "ConfirmFrame": false, "Acknowledge": false}
	for _, endpoint := range manifest.Endpoints {
		if _, needed := required[endpoint.Handler]; !needed {
			continue
		}
		required[endpoint.Handler] = true
		key := endpoint.Method + " " + endpoint.Path
		if !registered[key] {
			t.Errorf("Capture dependency missing from actual router: %s", key)
		}
	}
	for handler, found := range required {
		if !found {
			t.Errorf("Capture dependency missing from production contract: %s", handler)
		}
	}
}
