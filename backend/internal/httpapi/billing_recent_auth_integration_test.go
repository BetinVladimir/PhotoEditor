package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
)

func recentHTTPFixture(t *testing.T) (http.Handler, *pgxpool.Pool, string, *auth.Service) {
	t.Helper()
	dsn := os.Getenv("PHOTO_TEST_RUNTIME_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated auth runtime required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("isolated runtime invalid")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_auth"
	database.GuardApplicationRole(cfg)
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal("isolated runtime unavailable")
	}
	t.Cleanup(pool.Close)
	var service *auth.Service
	handler, admin, token, _ := httpFixtureWithServerOptions(t, nil, nil, nil, func(s *Server) { s.Auth.DB = pool; service = s.Auth })
	return handler, admin, token, service
}
func recentHTTPRequest(handler http.Handler, path, body, token, key, nonce string, decorate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Photo-Client", "native")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if nonce != "" {
		req.Header.Set("X-Photo-Auth-Nonce", nonce)
	}
	if decorate != nil {
		decorate(req)
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	return out
}
func recentHTTPMailCode(t *testing.T, admin *pgxpool.Pool, service *auth.Service, id uuid.UUID) string {
	t.Helper()
	var payload uuid.UUID
	var key string
	var ciphertext []byte
	if err := admin.QueryRow(context.Background(), "SELECT id,key_id,payload_cipher FROM photo.mail_payloads WHERE challenge_id=$1", id).Scan(&payload, &key, &ciphertext); err != nil {
		t.Fatal("challenge mail missing")
	}
	plain, err := service.MailKeys.Open(key, ciphertext, []byte(payload.String()+":payload"))
	if err != nil {
		t.Fatal("challenge mail unavailable")
	}
	var value struct{ Kind, Code, Purpose string }
	if json.Unmarshal(plain, &value) != nil || value.Kind != "recent_auth" || value.Purpose != "delete_profile" || !auth.ValidOTP(value.Code) {
		t.Fatal("wrong sensitive mail payload")
	}
	return value.Code
}
func recentVerifyBody(t *testing.T, id uuid.UUID, code string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"purpose": "delete_profile", "challenge_id": id.String(), "code": code})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
func TestIntegrationRecentAuthActualHTTP(t *testing.T) {
	handler, admin, token, service := recentHTTPFixture(t)
	nonce := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	requestKey := uuid.NewString()
	challengeResponse := recentHTTPRequest(handler, "/api/v1/me/recent-auth/challenges", `{"purpose":"delete_profile"}`, token, requestKey, nonce, nil)
	if challengeResponse.Code != 202 {
		t.Fatalf("challenge status %d", challengeResponse.Code)
	}
	var challenge auth.Challenge
	if json.Unmarshal(challengeResponse.Body.Bytes(), &challenge) != nil || challenge.ID == uuid.Nil || challenge.ExpiresIn != 300 {
		t.Fatal("invalid challenge DTO")
	}
	again := recentHTTPRequest(handler, "/api/v1/me/recent-auth/challenges", `{"purpose":"delete_profile"}`, token, requestKey, nonce, nil)
	if again.Code != 202 || again.Body.String() != challengeResponse.Body.String() {
		t.Fatal("challenge retry changed")
	}
	code := recentHTTPMailCode(t, admin, service, challenge.ID)
	// The regular sign-in endpoint cannot accept this purpose-specific code.
	loginBody, _ := json.Marshal(map[string]string{"challenge_id": challenge.ID.String(), "code": code, "client_type": "native"})
	if reply := recentHTTPRequest(handler, "/api/v1/auth/otp/verify", string(loginBody), "", "", "", nil); reply.Code != 401 {
		t.Fatal("purpose OTP accepted as login")
	}
	wrong := "000000"
	if code == wrong {
		wrong = "000001"
	}
	if reply := recentHTTPRequest(handler, "/api/v1/me/recent-auth", recentVerifyBody(t, challenge.ID, wrong), token, uuid.NewString(), nonce, nil); reply.Code != 422 {
		t.Fatal("wrong OTP should not trigger session refresh", reply.Code)
	}
	verifyKey := uuid.NewString()
	body := recentVerifyBody(t, challenge.ID, code)
	first := recentHTTPRequest(handler, "/api/v1/me/recent-auth", body, token, verifyKey, nonce, nil)
	if first.Code != 201 {
		t.Fatalf("proof status %d", first.Code)
	}
	var fields map[string]any
	var grant auth.RecentAuthGrant
	if json.Unmarshal(first.Body.Bytes(), &fields) != nil || len(fields) != 2 || json.Unmarshal(first.Body.Bytes(), &grant) != nil || grant.Grant == "" || grant.ExpiresAt.IsZero() {
		t.Fatal("proof response exposes wrong fields")
	}
	replay := recentHTTPRequest(handler, "/api/v1/me/recent-auth", body, token, verifyKey, nonce, nil)
	if replay.Code != 201 || replay.Body.String() != first.Body.String() {
		t.Fatal("proof retry changed")
	}
	who, err := service.AuthenticateAccess(context.Background(), token)
	if err != nil {
		t.Fatal("OTP changed login session")
	}
	var sessions int
	if err = admin.QueryRow(context.Background(), "SELECT count(*) FROM photo.sessions WHERE user_id=$1", who.UserID).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatal("second logged-in identity/session created")
	}
	for _, tc := range []struct {
		key, nonce, body string
		status           int
	}{
		{"", nonce, body, 400}, {uuid.NewString(), "", body, 422}, {uuid.NewString(), nonce, `{"purpose":"delete_profile","proof_hash":"forged"}`, 422},
	} {
		reply := recentHTTPRequest(handler, "/api/v1/me/recent-auth", tc.body, token, tc.key, tc.nonce, nil)
		if reply.Code != tc.status {
			t.Fatalf("invalid proof request status %d want %d", reply.Code, tc.status)
		}
	}
	duplicate := recentHTTPRequest(handler, "/api/v1/me/recent-auth", body, token, verifyKey, nonce, func(r *http.Request) { r.Header.Add("X-Photo-Auth-Nonce", nonce) })
	if duplicate.Code != 422 {
		t.Fatal("duplicate recovery nonce accepted")
	}
}
func TestIntegrationRecentAuthWebCSRFIsSeparateFromRecoveryNonce(t *testing.T) {
	handler, admin, token, service := recentHTTPFixture(t)
	ctx := context.Background()
	who, err := service.AuthenticateAccess(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	const csrf = "web-csrf-fixture"
	if _, err = admin.Exec(ctx, "UPDATE photo.sessions SET client_type='web',csrf_hash=$2 WHERE id=$1", who.SessionID, auth.HashSecret(csrf)); err != nil {
		t.Fatal(err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	web := func(csrf string) func(*http.Request) {
		return func(r *http.Request) {
			r.Header.Del("X-Photo-Client")
			r.Header.Set("Origin", "http://localhost:8080")
			if csrf != "" {
				r.Header.Set("X-CSRF-Token", csrf)
			}
		}
	}
	denied := recentHTTPRequest(handler, "/api/v1/me/recent-auth/challenges", `{"purpose":"delete_profile"}`, token, uuid.NewString(), nonce, web(""))
	if denied.Code != 403 {
		t.Fatal("recovery nonce bypassed browser CSRF", denied.Code)
	}
	reply := recentHTTPRequest(handler, "/api/v1/me/recent-auth/challenges", `{"purpose":"delete_profile"}`, token, uuid.NewString(), nonce, web(csrf))
	if reply.Code != 202 {
		t.Fatal("web challenge failed", reply.Code)
	}
	var challenge auth.Challenge
	if json.Unmarshal(reply.Body.Bytes(), &challenge) != nil {
		t.Fatal("bad challenge")
	}
	code := recentHTTPMailCode(t, admin, service, challenge.ID)
	body := recentVerifyBody(t, challenge.ID, code)
	key := uuid.NewString()
	first := recentHTTPRequest(handler, "/api/v1/me/recent-auth", body, token, key, nonce, web(csrf))
	if first.Code != 201 {
		t.Fatal("web proof failed", first.Code)
	}
	const renewed = "renewed-web-csrf-fixture"
	if _, err = admin.Exec(ctx, "UPDATE photo.sessions SET csrf_hash=$2 WHERE id=$1", who.SessionID, auth.HashSecret(renewed)); err != nil {
		t.Fatal(err)
	}
	replay := recentHTTPRequest(handler, "/api/v1/me/recent-auth", body, token, key, nonce, web(renewed))
	if replay.Code != 201 || replay.Body.String() != first.Body.String() {
		t.Fatal("CSRF renewal changed independent recovery binding", replay.Code)
	}
}
