package httpapi

import (
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRenderRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("render-route-fixture")}}
	response := httptest.NewRecorder()
	s.Routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/tenants/00000000-0000-4000-8000-000000000001/projects/00000000-0000-4000-8000-000000000002/render-batches", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("render route missing or accepted unauthenticated request", response.Code)
	}
}

func TestRenderFreezeRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("render-freeze-fixture")}}
	response := httptest.NewRecorder()
	s.Routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/tenants/00000000-0000-4000-8000-000000000001/assets/00000000-0000-4000-8000-000000000002/v2-draft/freeze", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("freeze route absent or unauthenticated", response.Code)
	}
}

func TestRenderInputSnapshotRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("render-input-fixture")}}
	response := httptest.NewRecorder()
	s.Routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/tenants/00000000-0000-4000-8000-000000000001/projects/00000000-0000-4000-8000-000000000002/render-input-snapshots", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("input snapshot route missing/auth bypass", response.Code)
	}
}

func TestRenderPolicyRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("render-policy-fixture")}}
	response := httptest.NewRecorder()
	s.Routes().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/tenants/00000000-0000-4000-8000-000000000001/projects/00000000-0000-4000-8000-000000000002/render-policy", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("policy route missing/auth bypass", response.Code)
	}
}
