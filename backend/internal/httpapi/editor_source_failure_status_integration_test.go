package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	runservice "photoeditor/backend/internal/run"
)

// Actual SQL-seeded isolated relational failures + real signed-session
// authentication, production GET route and run/project/tenant authorization.
// Not OTP provisioning, physical source read, worker or quality acceptance.
func TestIntegrationEditorSourceFailurePublicStatus(t *testing.T) {
	path, dsn := os.Getenv("PHOTO_EDITOR_CARRIER_HTTP_FIXTURES"), os.Getenv("PHOTO_TEST_DATABASE_URL")
	if path == "" || dsn == "" {
		t.Skip("requires owned isolated carrier fixture clone")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal("private fixture read")
	}
	type actor struct {
		User    uuid.UUID `json:"user"`
		Session uuid.UUID `json:"session"`
		Epoch   int64     `json:"epoch"`
	}
	type fixture struct {
		Tenant  uuid.UUID `json:"tenant"`
		Run     uuid.UUID `json:"run"`
		Final   uuid.UUID `json:"final"`
		Code    string    `json:"code"`
		Owner   actor     `json:"owner"`
		Foreign actor     `json:"foreign"`
	}
	var fixtures []fixture
	if e = json.Unmarshal(raw, &fixtures); e != nil || len(fixtures) != 3 {
		t.Fatal("closed three-fixture inventory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "photo_editor_carrier_") {
		t.Fatal("isolated clone only")
	}
	admin, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("fixture reader")
	}
	defer admin.Close()
	cfg, e = pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal("API config")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	cfg.MaxConns = 2
	api, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("API role pool")
	}
	defer api.Close()
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	signer := auth.TokenSigner{Active: "owned-carrier-http", Private: priv, Public: map[string]ed25519.PublicKey{"owned-carrier-http": pub}, Issuer: "owned-carrier-http", Audience: "api"}
	s := &Server{DB: api, Auth: &auth.Service{DB: admin, Signer: signer, Pepper: []byte(strings.Repeat("p", 32))}}
	handler := s.Routes()
	request := func(f fixture, a actor) *httptest.ResponseRecorder {
		token, e := signer.Issue(a.User, a.Session, a.Epoch)
		if e != nil {
			t.Fatal("actual session token issue")
		}
		req := httptest.NewRequest("GET", "/api/v1/tenants/"+f.Tenant.String()+"/runs/"+f.Run.String(), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	for _, f := range fixtures {
		t.Run(f.Code, func(t *testing.T) {
			switch f.Code {
			case "EDITOR_INTERMEDIATE_UNAVAILABLE", "EDITOR_SOURCE_OBJECT_MISSING", "EDITOR_SOURCE_OBJECT_INTEGRITY":
			default:
				t.Fatal("unapproved reason code")
			}
			out := request(f, f.Owner)
			if out.Code != 200 {
				t.Fatalf("authorized real GET status %d", out.Code)
			}
			var status runservice.Status
			if json.Unmarshal(out.Body.Bytes(), &status) != nil || status.ID != f.Run || len(status.Errors) != 1 || status.Errors[0] != f.Code {
				t.Fatal("exact typed public errors")
			}
			// Error transport contains only the known code, never locator/issuer data.
			rawErrors, _ := json.Marshal(status.Errors)
			if string(rawErrors) != "[\""+f.Code+"\"]" {
				t.Fatal("unbounded error metadata")
			}
			child := f
			child.Run = f.Final
			childOut := request(child, f.Owner)
			if childOut.Code != 200 {
				t.Fatalf("authorized real final child GET status %d", childOut.Code)
			}
			var childStatus runservice.Status
			if json.Unmarshal(childOut.Body.Bytes(), &childStatus) != nil || childStatus.Kind != "v2" || len(childStatus.Errors) != 1 || childStatus.Errors[0] != f.Code {
				t.Fatal("typed final v2 child errors")
			}
			childDenied := request(child, f.Foreign)
			if childDenied.Code != 403 && childDenied.Code != 404 {
				t.Fatal("foreign final child read allowed")
			}
			denied := request(f, f.Foreign)
			if denied.Code != 403 && denied.Code != 404 {
				t.Fatalf("foreign tenant actor was not denied: %d", denied.Code)
			}
			var secret bool
			if admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.runs WHERE id=$1 AND config_snapshot ? 'errors')`, f.Run).Scan(&secret) != nil || secret {
				t.Fatal("no arbitrary config error carrier")
			}
		})
	}
}
