package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/style"
	"photoeditor/backend/internal/styleupload"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/workflow"
	"regexp"
	"testing"
	"time"
)

// Actual OTP/session auth, restricted logins, HTTP handlers and versioned S3 upload.
func TestIntegrationStyleImportClaimUsesUnambiguousTargetInRealPipeline(t *testing.T) {
	if os.Getenv("PHOTO_STYLE_KIND_OWNER_DSN") == "" {
		t.Skip("isolated style-kind DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	check := func(stage string, e error) {
		t.Helper()
		if e != nil {
			var pg *pgconn.PgError
			if errors.As(e, &pg) {
				t.Fatalf("%s SQLSTATE=%s schema=%s table=%s column=%s constraint=%s routine=%s denied_object=%v safe_frames=%v", stage, pg.Code, pg.SchemaName, pg.TableName, pg.ColumnName, pg.ConstraintName, pg.Routine, regexp.MustCompile(`permission denied for (?:table|function|schema) ([a-zA-Z0-9_.]+)`).FindStringSubmatch(pg.Message), regexp.MustCompile(`PL/pgSQL function ([A-Za-z_0-9.(), ]+) line ([0-9]+)`).FindAllString(pg.Where, -1))
			}
			t.Fatalf("%s error_class=%T", stage, e)
		}
	}
	owner, e := pgxpool.New(ctx, os.Getenv("PHOTO_STYLE_KIND_OWNER_DSN"))
	check("owner", e)
	defer owner.Close()
	var name string
	check("database", owner.QueryRow(ctx, "select current_database()").Scan(&name))
	if name != "photoeditor_style_claim_r1" && name != "photoeditor_style_claim_r2" && name != "photoeditor_style_claim_r3" {
		t.Fatal("isolated DB refused")
	}
	app := func(key, role string) *pgxpool.Pool {
		c, e := pgxpool.ParseConfig(os.Getenv(key))
		check("app-config", e)
		c.ConnConfig.RuntimeParams["role"] = role
		c.MaxConns = 3
		database.GuardApplicationRole(c)
		p, e := pgxpool.NewWithConfig(ctx, c)
		check("app-pool", e)
		check("app-role", database.RequireApplicationRole(ctx, p, role))
		var restricted bool
		check("app-login", p.QueryRow(ctx, "select not rolsuper and not rolbypassrls and not rolcreatedb and not rolcreaterole from pg_roles where rolname=session_user").Scan(&restricted))
		if !restricted {
			t.Fatal("privileged app login")
		}
		t.Cleanup(p.Close)
		return p
	}
	api, adb := app("PHOTO_STYLE_KIND_API_DSN", "photo_api"), app("PHOTO_STYLE_KIND_AUTH_DSN", "photo_auth")
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	check("key", e)
	key := make([]byte, 32)
	_, e = rand.Read(key)
	check("mail-key", e)
	ring := auth.KeyRing{Active: "isolated", Keys: map[string][]byte{"isolated": key}}
	a := &auth.Service{DB: adb, Pepper: key, MailKeys: ring, Signer: auth.TokenSigner{Active: "isolated", Private: priv, Public: map[string]ed25519.PublicKey{"isolated": pub}, Issuer: "isolated", Audience: "api"}, Registration: true}
	challenge, e := a.RequestOTP(ctx, uuid.NewString()+"@example.test", "en", uuid.NewString())
	check("request-otp", e)
	var mid uuid.UUID
	var kid string
	var cipher []byte
	check("encrypted-mail", adb.QueryRow(ctx, "select id,key_id,payload_cipher from photo.mail_payloads where challenge_id=$1", challenge.ID).Scan(&mid, &kid, &cipher))
	plain, e := ring.Open(kid, cipher, []byte(mid.String()+":payload"))
	check("mail-delivery", e)
	var mail map[string]string
	check("mail-json", json.Unmarshal(plain, &mail))
	token, e := a.VerifyOTP(ctx, challenge.ID, mail["code"], "native")
	check("verify-otp", e)
	who, e := a.AuthenticateAccess(ctx, token.Access)
	check("authenticate", e)
	ten, e := (&tenant.Service{DB: api, MailKeys: ring}).Create(ctx, who, uuid.NewString(), tenant.CreateRequest{Name: "Actual style-kind regression"})
	check("tenant", e)
	p, e := (&project.Service{DB: api}).Create(ctx, who, ten.ID, uuid.NewString(), project.Create{Name: "Upload import", Visibility: "workspace", DownloadAllowed: true})
	check("project", e)
	if p.ResourceID == nil {
		t.Fatal("missing project")
	}
	pid := *p.ResourceID
	bucket := "photo-style-kind-" + uuid.NewString()
	store, e := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), PublicEndpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	check("store", e)
	check("bootstrap", store.Bootstrap(ctx))
	handler := (&Server{DB: api, Auth: a, Media: &media.Service{DB: api, Objects: store, Bucket: bucket}, Config: config.Config{MaxJSONBytes: 1 << 20, Environment: "development"}}).Routes()
	call := func(path string, body any, key string, status int) []byte {
		t.Helper()
		b, e := json.Marshal(body)
		check("body", e)
		r := httptest.NewRequest("POST", path, bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer "+token.Access)
		r.Header.Set("X-Photo-Client", "native")
		r.Header.Set("X-CSRF-Token", token.CSRF)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("HTTP wanted=%d actual=%d", status, w.Code)
		}
		return w.Body.Bytes()
	}
	raw, e := os.ReadFile(os.Getenv("PHOTO_STYLE_KIND_PACKAGE"))
	check("package", e)
	sum := sha256.Sum256(raw)
	root := "/api/v1/tenants/" + ten.ID.String()
	uploads := root + "/projects/" + pid.String() + "/style-package-uploads"
	var upload styleupload.Session
	check("begin", json.Unmarshal(call(uploads, styleupload.BeginRequest{Filename: "style.json", Bytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}, uuid.NewString(), 200), &upload))
	var parts struct {
		Parts []objectstore.PartURL `json:"parts"`
	}
	check("parts", json.Unmarshal(call(uploads+"/"+upload.ID.String()+"/parts", map[string]any{"part_numbers": []int{1}}, "", 200), &parts))
	if len(parts.Parts) != 1 {
		t.Fatal("part inventory")
	}
	req, e := http.NewRequestWithContext(ctx, "PUT", parts.Parts[0].URL, bytes.NewReader(raw))
	check("put", e)
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(req)
	check("S3 PUT", e)
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode/100 != 2 || response.Header.Get("ETag") == "" {
		t.Fatal("part not stored")
	}
	var ready styleupload.Session
	check("complete", json.Unmarshal(call(uploads+"/"+upload.ID.String()+"/complete", styleupload.CompleteRequest{ExpectedSize: int64(len(raw)), Parts: []objectstore.UploadedPart{{PartNumber: 1, ETag: response.Header.Get("ETag")}}}, uuid.NewString(), 200), &ready))
	if ready.State != "ready" || ready.Artifact == nil {
		t.Fatal("package not genuinely ready")
	}

	input := style.ImportRequest{ProjectID: pid, Name: "Actual claim regression", UploadArtifactID: *ready.Artifact}
	keyImport := uuid.NewString()
	accepted := call(root+"/styles/import", input, keyImport, 202)
	var result struct {
		RunID uuid.UUID `json:"run_id"`
	}
	check("accepted", json.Unmarshal(accepted, &result))
	scheduler := app("PHOTO_STYLE_CLAIM_SCHEDULER_DSN", "photo_scheduler")
	var claimed uuid.UUID
	e = scheduler.QueryRow(ctx, "select run_id from photo.claim_style_import_build()").Scan(&claimed)
	var pg *pgconn.PgError
	if !errors.As(e, &pg) || pg.Code != "42702" || pg.Routine != "plpgsql_post_column_ref" {
		t.Fatal("nonempty baseline not actual ambiguity")
	}
	var before string
	const metadata = `select (to_jsonb(p)-'prosrc')::text from pg_proc p where oid='photo.claim_style_import_build()'::regprocedure`
	check("before-meta", owner.QueryRow(ctx, metadata).Scan(&before))
	migration, e := os.ReadFile(os.Getenv("PHOTO_STYLE_KIND_MIGRATION"))
	check("migration", e)
	_, e = owner.Exec(ctx, string(bytes.Split(migration, []byte("-- +goose Down"))[0]))
	check("candidate-up", e)
	var after string
	check("after-meta", owner.QueryRow(ctx, metadata).Scan(&after))
	if before != after {
		t.Fatal("function authority metadata changed")
	}
	// Existing restricted scheduler must still have no direct artifacts SELECT.
	_, e = scheduler.Exec(ctx, "select id from photo.artifacts limit 1")
	if !errors.As(e, &pg) || pg.Code != "42501" {
		t.Fatal("broad artifact privilege unexpectedly present")
	}
	accessor, e := os.ReadFile(os.Getenv("PHOTO_STYLE_CLAIM_ACCESSOR_MIGRATION"))
	check("accessor-migration", e)
	_, e = owner.Exec(ctx, string(bytes.Split(accessor, []byte("-- +goose Down"))[0]))
	check("accessor-up", e)
	coordinator := workflow.StyleImportCoordinator{Store: workflow.StyleImportStore{DB: scheduler}, Publisher: artifactjson.Publisher{Repository: workflow.StyleImportDocuments{DB: scheduler, Bucket: bucket}, Objects: store}}
	done, e := coordinator.ProcessOne(ctx)
	check("actual-coordinator", e)
	if !done {
		t.Fatal("nonempty coordinator did not materialize")
	}
	var buildState, taskState, operation, runState string
	var paramsID uuid.UUID
	var paramsSHA string
	check("published-task", owner.QueryRow(ctx, `select b.state,t.state,t.operation,r.status,t.params_artifact_id,a.sha256 from photo.style_import_builds b join photo.tasks t on t.tenant_id=b.tenant_id and t.id=b.task_id join photo.runs r on r.tenant_id=b.tenant_id and r.id=b.run_id join photo.artifacts a on a.tenant_id=t.tenant_id and a.id=t.params_artifact_id where b.run_id=$1`, result.RunID).Scan(&buildState, &taskState, &operation, &runState, &paramsID, &paramsSHA))
	if buildState != "complete" || taskState != "ready" || operation != "style.import" || runState != "running" || paramsID == uuid.Nil || len(paramsSHA) != 64 {
		t.Fatal("actual pipeline incomplete")
	}
	done, e = coordinator.ProcessOne(ctx)
	check("empty-replay", e)
	if done {
		t.Fatal("complete build reclaimed")
	}
	var n int
	check("single-task", owner.QueryRow(ctx, "select count(*) from photo.tasks where tenant_id=$1 and run_id=$2", ten.ID, result.RunID).Scan(&n))
	if n != 1 {
		t.Fatal("duplicate tasks")
	}
	t.Log("ACTUAL_GENUINE_HTTP_IMPORT_RESTRICTED_SCHEDULER_COORDINATOR_PUBLICATION_MATERIALIZATION_PASS")
}
