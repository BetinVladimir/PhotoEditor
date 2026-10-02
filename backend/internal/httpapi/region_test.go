package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRegionRouteRegisteredAndRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("region-route-fixture")}}
	handler := s.Routes()
	found := false
	err := chi.Walk(handler.(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "POST" && path == "/api/v1/tenants/{t}/assets/{id}/region-previews" {
			found = true
		}
		return nil
	})
	if err != nil || !found {
		t.Fatal("region route missing", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/v1/tenants/00000000-0000-0000-0000-000000000001/assets/00000000-0000-0000-0000-000000000002/region-previews", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated region request accepted", response.Code)
	}
}

func TestRegionDescriptorRouteRegisteredAndRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("region-descriptor-fixture")}}
	handler := s.Routes()
	found := false
	err := chi.Walk(handler.(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "GET" && path == "/api/v1/tenants/{t}/assets/{id}/region-previews/{run}/descriptor" {
			found = true
		}
		return nil
	})
	if err != nil || !found {
		t.Fatal("descriptor route missing", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/tenants/00000000-0000-4000-8000-000000000001/assets/00000000-0000-4000-8000-000000000002/region-previews/00000000-0000-4000-8000-000000000003/descriptor", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated descriptor read accepted", response.Code)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("descriptor is cacheable")
	}
}
