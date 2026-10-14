package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/config"
	"testing"
	"time"
)

func httpFixtureWithRegion(t *testing.T, region RegionQueue) (http.Handler, *pgxpool.Pool, string, uuid.UUID) {
	return httpFixtureWithQueues(t, region, nil)
}

func httpFixtureWithQueues(t *testing.T, region RegionQueue, render RenderQueue) (http.Handler, *pgxpool.Pool, string, uuid.UUID) {
	return httpFixtureWithRenderFreezer(t, region, render, nil)
}

func httpFixtureWithRenderFreezer(t *testing.T, region RegionQueue, render RenderQueue, freezer RenderFreezer) (http.Handler, *pgxpool.Pool, string, uuid.UUID) {
	t.Helper()
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	pool := func(role string) *pgxpool.Pool {
		cfg, e := pgxpool.ParseConfig(dsn)
		if e != nil {
			t.Fatal(e)
		}
		cfg.ConnConfig.RuntimeParams["role"] = role
		p, e := pgxpool.NewWithConfig(ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(p.Close)
		return p
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	key := make([]byte, 32)
	rand.Read(key)
	a := &auth.Service{DB: pool("photo_auth"), Pepper: key, MailKeys: auth.KeyRing{Active: "test", Keys: map[string][]byte{"test": key}}, Signer: auth.TokenSigner{Active: "test", Private: priv, Public: map[string]ed25519.PublicKey{"test": pub}, Issuer: "test", Audience: "test"}, Registration: true}
	var user uuid.UUID
	var epoch int64
	if e := admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@example.test").Scan(&user, &epoch); e != nil {
		t.Fatal(e)
	}
	sid := uuid.New()
	_, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',$5)", sid, user, auth.HashSecret(uuid.NewString()), uuid.New(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	access, err := a.Signer.Issue(user, sid, epoch)
	if err != nil {
		t.Fatal(err)
	}
	var tid uuid.UUID
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tid); err != nil {
		t.Fatal(err)
	}
	return (&Server{DB: pool("photo_api"), Auth: a, RegionQueue: region, RenderQueue: render, RenderFreezer: freezer, Config: config.Config{Environment: "development", CaptureOrigin: "http://localhost:3333", AppOrigin: "http://localhost:8080", PortalOrigin: "http://localhost:3000", MaxJSONBytes: 1048576}}).Routes(), admin, access, tid
}
func httpFixture(t *testing.T) (http.Handler, *pgxpool.Pool, string, uuid.UUID) {
	return httpFixtureWithRegion(t, nil)
}
func request(handler http.Handler, method, path, body, token, key, rev string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Photo-Client", "native")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if rev != "" {
		r.Header.Set("If-Match", rev)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func TestIntegrationProjectHTTP(t *testing.T) {
	h, _, token, tid := httpFixture(t)
	base := "/api/v1/tenants/" + tid.String() + "/projects"
	body := `{"name":"Portraits","description":"sample","visibility":"restricted","download_allowed":true}`
	w := request(h, "POST", base, body, token, "create-project", "")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	first := w.Body.String()
	var p struct {
		ID       string `json:"id"`
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	w = request(h, "POST", base, body, token, "create-project", "")
	if w.Code != 201 || w.Body.String() != first {
		t.Fatal("replay differs", w.Code, w.Body.String())
	}
	w = request(h, "POST", base, `{"name":"Other","description":"sample","visibility":"restricted","download_allowed":true}`, token, "create-project", "")
	if w.Code != 409 {
		t.Fatal("idempotency conflict", w.Code, w.Body.String())
	}
	w = request(h, "GET", base+"/"+p.ID, "", token, "", "")
	if w.Code != 200 || w.Header().Get("ETag") != `"1"` {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "PATCH", base+"/"+p.ID, `{"name":"Changed"}`, token, "", `"1"`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "PATCH", base+"/"+p.ID, `{"name":"Stale"}`, token, "", `"1"`)
	if w.Code != 409 {
		t.Fatal("revision", w.Code, w.Body.String())
	}
	w = request(h, "PATCH", base+"/"+p.ID, `{"name":"Missing revision"}`, token, "", "")
	if w.Code != 428 {
		t.Fatal("precondition", w.Code)
	}
	w = request(h, "GET", "/api/v1/tenants/"+uuid.NewString()+"/projects", "", token, "", "")
	if w.Code != 404 {
		t.Fatal("tenant hidden", w.Code, w.Body.String())
	}
	w = request(h, "GET", base, "", token, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestIntegrationBrowserCSRF(t *testing.T) {
	h, _, _, _ := httpFixture(t)
	r := httptest.NewRequest("GET", "/api/v1/auth/csrf", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var data struct {
		Token string `json:"csrf_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &data)
	cookies := w.Result().Cookies()
	r = httptest.NewRequest("POST", "/api/v1/auth/otp/request", bytes.NewBufferString(`{"email":"test@example.test","locale":"en"}`))
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", data.Token)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", "/api/v1/auth/otp/request", bytes.NewBufferString(`{"email":"test@example.test","locale":"en"}`))
	r.Header.Set("Origin", "http://evil.example")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin", w.Code)
	}
}
