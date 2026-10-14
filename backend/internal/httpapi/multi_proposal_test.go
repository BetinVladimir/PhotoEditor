package httpapi

import (
	"github.com/google/uuid"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestMultiProposalRoutesRequireAuthenticationAndNoStore(t *testing.T) {
	for _, path := range []string{"panorama-proposals", "focus-stack-proposals"} {
		t.Run(path, func(t *testing.T) {
			s := &Server{Auth: &auth.Service{Pepper: []byte("multi-proposal-route")}}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/"+path+"/"+uuid.NewString(), nil)
			s.Routes().ServeHTTP(w, r)
			if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unauthorized candidate: %d %s", w.Code, w.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestMultiPreviewRoutesRequireAuthenticationAndNoStore(t *testing.T) {
	for _, path := range []string{"panorama-proposals", "focus-stack-proposals"} {
		for _, variant := range []string{"preview1600", "preview2560"} {
			s := &Server{Auth: &auth.Service{Pepper: []byte("multi-preview-route")}}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/"+path+"/"+uuid.NewString()+"/previews/"+variant, nil)
			s.Routes().ServeHTTP(w, r)
			if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("unauthorized preview", w.Code, w.Header())
			}
		}
	}
}

func TestMultiQualityRoutesRequireAuthenticationAndNoStore(t *testing.T) {
	for _, path := range []string{"panorama-proposals", "focus-stack-proposals"} {
		for _, variant := range []string{"quality"} {
			s := &Server{Auth: &auth.Service{Pepper: []byte("multi-preview-route")}}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/"+path+"/"+uuid.NewString()+"/"+variant, nil)
			s.Routes().ServeHTTP(w, r)
			if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("unauthorized preview", w.Code, w.Header())
			}
		}
	}
}

func TestMultiApplyRoutesRequireAuthenticationAndNoStore(t *testing.T) {
	for _, path := range []string{"panorama-proposals", "focus-stack-proposals"} {
		for _, variant := range []string{"apply"} {
			s := &Server{Auth: &auth.Service{Pepper: []byte("multi-preview-route")}}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/"+path+"/"+uuid.NewString()+"/"+variant, nil)
			s.Routes().ServeHTTP(w, r)
			if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("unauthorized preview", w.Code, w.Header())
			}
		}
	}
}
