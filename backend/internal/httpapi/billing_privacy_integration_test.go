package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/privacy"
)

func TestIntegrationPrivacyPreferencesActualRoute(t *testing.T) {
	var authentication *auth.Service
	handler, admin, token, _ := httpFixtureWithServerOptions(t, nil, nil, nil, func(s *Server) {
		authentication = s.Auth
		s.PrivacyPreferences = privacy.PostgresPreferences{DB: s.Auth.DB}
	})
	key := uuid.NewString()
	first := request(handler, http.MethodPatch, "/api/v1/me/privacy", `{"analytics":true}`, token, key, `"1"`)
	if first.Code != 200 {
		t.Fatalf("first update status %d: %s", first.Code, first.Body.String())
	}
	var original privacy.Preferences
	if err := json.Unmarshal(first.Body.Bytes(), &original); err != nil || original.Revision != "2" || !original.Analytics || original.Marketing {
		t.Fatal("invalid preferences", err, first.Body.String())
	}
	replay := request(handler, http.MethodPatch, "/api/v1/me/privacy", `{"analytics":true}`, token, key, `"1"`)
	if replay.Code != 200 || replay.Body.String() != first.Body.String() {
		t.Fatal("replay changed", replay.Code, replay.Body.String())
	}
	conflict := request(handler, http.MethodPatch, "/api/v1/me/privacy", `{"analytics":false}`, token, key, `"1"`)
	if conflict.Code != 409 {
		t.Fatal("changed key payload accepted", conflict.Code)
	}
	stale := request(handler, http.MethodPatch, "/api/v1/me/privacy", `{"marketing":true}`, token, uuid.NewString(), `"1"`)
	if stale.Code != 409 {
		t.Fatal("stale revision accepted", stale.Code)
	}
	withdrawal := request(handler, http.MethodPatch, "/api/v1/me/privacy", `{"analytics":false}`, token, uuid.NewString(), `"2"`)
	if withdrawal.Code != 200 {
		t.Fatal("withdrawal failed", withdrawal.Code)
	}
	var count int
	if err := admin.QueryRow(context.Background(), "SELECT count(*) FROM photo.account_legal_events").Scan(&count); err != nil || count != 2 {
		t.Fatal("audit effects", count, err)
	}
	for _, tc := range []struct {
		body, token, key, rev string
		status                int
	}{
		{`{}`, token, uuid.NewString(), `"3"`, 422},
		{`{"analytics":true,"unknown":true}`, token, uuid.NewString(), `"3"`, 422},
		{`{"analytics":true}`, "", uuid.NewString(), `"3"`, 401},
		{`{"analytics":true}`, token, uuid.NewString(), "", 428},
	} {
		out := request(handler, http.MethodPatch, "/api/v1/me/privacy", tc.body, tc.token, tc.key, tc.rev)
		if out.Code != tc.status {
			t.Fatalf("invalid request: got %d want %d", out.Code, tc.status)
		}
	}
	// Revocation is tested before any stored response can be replayed.
	who, err := authentication.AuthenticateAccess(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(context.Background(), "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	denied := request(handler, http.MethodPatch, "/api/v1/me/privacy", `{"analytics":true}`, token, key, `"1"`)
	if denied.Code != 401 {
		t.Fatal("revoked token replay accepted", denied.Code)
	}
}
