package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/privacy"
)

func TestIntegrationDeletionPreflightActualRoute(t *testing.T) {
	handler, admin, token, tenant := httpFixtureWithServerOptions(t, nil, nil, nil, func(s *Server) { s.PrivacyPreflight = privacy.PostgresPreferences{DB: s.Auth.DB} })
	key := uuid.NewString()
	first := request(handler, http.MethodPost, "/api/v1/me/deletion/preflight", `{}`, token, key, "")
	if first.Code != 200 {
		t.Fatalf("preflight status %d: %s", first.Code, first.Body.String())
	}
	var out privacy.DeletionPreflight
	if err := json.Unmarshal(first.Body.Bytes(), &out); err != nil || out.PrivacyCaseID == uuid.Nil || out.Revision != "1" || out.RequiredRecentAuthPurpose != "delete_profile" || len(out.OwnershipChoices) != 1 || out.OwnershipChoices[0].TenantID != tenant {
		t.Fatal("bad snapshot", err, first.Body.String())
	}
	replay := request(handler, http.MethodPost, "/api/v1/me/deletion/preflight", `{}`, token, key, "")
	if replay.Code != 200 || replay.Body.String() != first.Body.String() {
		t.Fatal("preflight replay changed", replay.Code)
	}
	var count int
	if err := admin.QueryRow(context.Background(), "SELECT count(*) FROM photo.privacy_cases WHERE case_id=$1 AND deadline_at>clock_timestamp()", out.PrivacyCaseID).Scan(&count); err != nil || count != 1 {
		t.Fatal("missing case deadline", count, err)
	}
	if err := admin.QueryRow(context.Background(), "SELECT count(*) FROM photo.profile_deletion_requests").Scan(&count); err != nil || count != 0 {
		t.Fatal("preflight accepted deletion", count, err)
	}
	for _, tc := range []struct {
		body, token, key string
		status           int
	}{
		{`{}`, token, "", 400}, {`{"user_id":"forged"}`, token, uuid.NewString(), 422}, {`{}`, "", uuid.NewString(), 401},
	} {
		reply := request(handler, http.MethodPost, "/api/v1/me/deletion/preflight", tc.body, tc.token, tc.key, "")
		if reply.Code != tc.status {
			t.Fatalf("invalid preflight status %d want %d", reply.Code, tc.status)
		}
	}
}
