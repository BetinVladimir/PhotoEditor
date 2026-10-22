package httpapi

import (
 "net/http"
 "net/http/httptest"
 "testing"
 "github.com/go-chi/chi/v5"
 "photoeditor/backend/internal/auth"
)

func TestAuthoredFaceMediaMountedOnActualServer(t *testing.T) {
 server := &Server{Auth: &auth.Service{}}
 handler := server.Routes()
 router, ok := handler.(chi.Routes)
 if !ok { t.Fatal("actual Server routes unavailable") }
 path := "/api/v1/tenants/{t}/authored-renders/{parent}/phases/{ordinal}/face-selection/{candidate}/media"
 matches := 0
 if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
  if method == http.MethodGet && route == path { matches++ }
  return nil
 }); err != nil { t.Fatal(err) }
 if matches != 1 { t.Fatalf("readonly preview route count %d", matches) }
 req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/00000000-0000-4000-8000-000000000001/authored-renders/00000000-0000-4000-8000-000000000002/phases/1/face-selection/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/media", nil)
 response := httptest.NewRecorder()
 handler.ServeHTTP(response, req)
 if response.Code != http.StatusUnauthorized { t.Fatalf("unauthenticated dependency-free request: %d", response.Code) }
 if response.Header().Get("Cache-Control") != "private, no-store" { t.Fatal("private transport lost") }
}
