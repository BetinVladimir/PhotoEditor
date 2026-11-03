package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/portal"
	videoapi "photoeditor/backend/internal/video/api"
)

type videoRoute struct{ method, path string }

func videoRoutes() []videoRoute {
	t, p, x := uuid.NewString(), uuid.NewString(), uuid.NewString()
	b := "/api/v1/tenants/" + t
	return []videoRoute{
		{"POST", b + "/projects/" + p + "/video/uploads"}, {"PUT", b + "/video/uploads/" + x + "/content"},
		{"GET", b + "/projects/" + p + "/video/media"}, {"GET", b + "/video/media/" + x + "/proxy"},
		{"POST", b + "/projects/" + p + "/video/sequences"}, {"GET", b + "/video/sequences/" + x},
		{"PUT", b + "/video/sequences/" + x + "/document"}, {"POST", b + "/video/sequences/" + x + "/renders"},
		{"GET", b + "/video/renders/" + x}, {"GET", b + "/video/renders/" + x + "/content"},
		{"POST", b + "/projects/" + p + "/video/deliveries"}, {"POST", b + "/video/deliveries/" + x + "/renders"},
		{"POST", b + "/video/deliveries/" + x + "/publish"}, {"POST", b + "/video/deliveries/" + x + "/shares"},
		{"DELETE", b + "/video/shares/" + x},
	}
}

func TestVideoRoutesRequireSession(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("video-routes")}, Video: &videoapi.Service{}}
	for _, r := range videoRoutes() {
		w := httptest.NewRecorder()
		s.Routes().ServeHTTP(w, newJSONTestRequest(r.method, r.path, strings.NewReader(`{}`)))
		if w.Code != 401 {
			t.Errorf("%s %s: status=%d", r.method, r.path, w.Code)
		}
	}
}

func TestVideoRoutesAbsentWithoutService(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("video-routes")}}
	for _, r := range videoRoutes() {
		w := httptest.NewRecorder()
		s.Routes().ServeHTTP(w, newJSONTestRequest(r.method, r.path, strings.NewReader(`{}`)))
		if w.Code != 404 && w.Code != 405 {
			t.Errorf("%s %s mounted without a video service: status=%d", r.method, r.path, w.Code)
		}
	}
}

func TestVideoViewerRoutesRequirePortalSession(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("video-routes")}, Portal: &portal.Service{}, Video: &videoapi.Service{}}
	for _, path := range []string{"/api/v1/portal/video/delivery", "/api/v1/portal/video/renders/" + uuid.NewString() + "/content",
		"/api/v1/portal/video/renders/" + uuid.NewString() + "/content?download=1"} {
		w := httptest.NewRecorder()
		s.Routes().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Errorf("GET %s: status=%d", path, w.Code)
		}
	}
	absent := &Server{Auth: &auth.Service{Pepper: []byte("video-routes")}, Portal: &portal.Service{}}
	w := httptest.NewRecorder()
	absent.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/portal/video/delivery", nil))
	if w.Code != 404 {
		t.Errorf("viewer route mounted without a video service: %d", w.Code)
	}
}
