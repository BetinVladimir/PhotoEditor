package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestAuthoredFaceSelectionRouteScopeExact(t *testing.T) {
	parent := uuid.New()
	sha := strings.Repeat("a", 64)
	for _, test := range []struct {
		ordinal, hash string
		valid         bool
	}{{"1", sha, true}, {"4998", sha, true}, {"0", sha, false}, {"4999", sha, false}, {"true", sha, false}, {"1", strings.Repeat("A", 64), false}, {"1", sha + "a", false}} {
		r := httptest.NewRequest("GET", "/", nil)
		route := chi.NewRouteContext()
		route.URLParams.Add("parent", parent.String())
		route.URLParams.Add("ordinal", test.ordinal)
		route.URLParams.Add("candidate", test.hash)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
		actual, ordinal, hash, err := authoredFaceSelectionScope(r)
		if (err == nil) != test.valid {
			t.Fatalf("scope %q/%q: %v", test.ordinal, test.hash, err)
		}
		if test.valid && (actual != parent || ordinal < 1 || hash != sha) {
			t.Fatal("route identity changed")
		}
	}
}
func TestAuthoredFaceDecisionWireRejectsUnknownKeys(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"revision":1,"decision":"accepted","selected_objects":[],"source":"client-controlled"}`))
	var body authoredFaceDecisionBody
	if decodeJSON(r, &body) == nil {
		t.Fatal("caller source accepted")
	}
}
