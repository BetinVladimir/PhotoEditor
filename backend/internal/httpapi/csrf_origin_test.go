package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/config"
)

// A separately hosted browser app is same-site, but not same-origin, with the
// API. Exercise the real route and bootstrap cookie without a database double.
func TestCSRFBootstrapOriginAndFetchMetadata(t *testing.T) {
	s := &Server{Config: config.Config{Environment: "development", AppOrigin: "http://localhost:19106", PortalOrigin: "http://localhost:13000"}, Auth: &auth.Service{Pepper: []byte("csrf-origin-regression-key-32bytes")}}
	h := s.Routes()
	for _, tc := range []struct {
		name, origin, site string
		status             int
	}{
		{"app_same_site", s.Config.AppOrigin, "same-site", http.StatusOK},
		{"portal_same_site", s.Config.PortalOrigin, "same-site", http.StatusOK},
		{"same_origin_without_origin", "", "same-origin", http.StatusOK},
		{"app_without_metadata", s.Config.AppOrigin, "", http.StatusOK},
		{"untrusted_same_site", "http://localhost:19000", "same-site", http.StatusForbidden},
		{"untrusted_same_origin_claim", "https://untrusted.example", "same-origin", http.StatusForbidden},
		{"no_origin_same_site", "", "same-site", http.StatusForbidden},
		{"no_origin_cross_site", "", "cross-site", http.StatusForbidden},
		{"app_cross_site", s.Config.AppOrigin, "cross-site", http.StatusForbidden},
		{"null_origin", "null", "same-site", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/csrf", nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.site != "" {
				r.Header.Set("Sec-Fetch-Site", tc.site)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			if tc.status != http.StatusOK {
				if len(w.Result().Cookies()) != 0 {
					t.Fatal("denied origin received cookie")
				}
				return
			}
			var body struct {
				Token string `json:"csrf_token"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Token == "" {
				t.Fatalf("missing bootstrap token: %v", err)
			}
			verify := httptest.NewRequest(http.MethodPost, "/api/v1/auth/otp/request", nil)
			verify.Header.Set("X-CSRF-Token", body.Token)
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != s.cookieName("csrf") || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
				t.Fatal("bootstrap cookie security changed")
			}
			verify.AddCookie(cookies[0])
			if !s.bootstrapValid(verify) {
				t.Fatal("issued token does not validate against issued cookie")
			}
		})
	}
}
