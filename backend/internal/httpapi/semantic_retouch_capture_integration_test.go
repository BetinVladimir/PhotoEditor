package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"net/url"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticreview"
	"strconv"
	"testing"
	"time"
)

// Extends the existing physically rendered CPU fixture. No accepted task/result,
// physical receipt or applied state is seeded by this HTTP driver.
func TestIntegrationRetouchSelectedHTTPCapture(t *testing.T) {
	file := os.Getenv("PHOTO_TEST_V27_RETOUCH_APPLY_FIXTURE")
	if file == "" || os.Getenv("PHOTO_TEST_DISPOSABLE") != "1" {
		t.Skip("owned physical retouch fixture required")
	}
	var f struct {
		Tenant  uuid.UUID                     `json:"tenant_id"`
		Intent  uuid.UUID                     `json:"intent_id"`
		Actor   uuid.UUID                     `json:"actor_id"`
		Session uuid.UUID                     `json:"session_id"`
		Target  uuid.UUID                     `json:"target_id"`
		SRGB    string                        `json:"srgb_sha"`
		Catalog json.RawMessage               `json:"catalog"`
		Fence   semanticreview.PreviewRequest `json:"preview_fence"`
	}
	raw, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&f); e != nil {
		t.Fatal(e)
	}
	catalog, e := semanticreview.DecodeExposurePreparationCatalog(f.Catalog)
	if e != nil || len(catalog.Tenants) != 1 || catalog.Tenants[0].TenantID != f.Tenant {
		t.Fatal("own catalog required", e)
	}
	entry := catalog.Tenants[0]
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := func(role string) *pgxpool.Pool {
		cfg, e := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if e != nil {
			t.Fatal(e)
		}
		cfg.ConnConfig.RuntimeParams["role"] = role
		db, e := pgxpool.NewWithConfig(ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(db.Close)
		return db
	}
	api, admission, authDB := pool("photo_api"), pool("photo_semantic_admission"), pool("photo_auth")
	bucket := os.Getenv("PHOTO_TEST_MINIO_BUCKET")
	objects, e := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if e != nil {
		t.Fatal(e)
	}
	lookup, e := semanticreview.SQLRetouchPreviewResolver(api, objects, bucket)
	if e != nil {
		t.Fatal(e)
	}
	lock, e := colorprofile.LoadLockSHA("../../../resources/icc")
	if e != nil {
		t.Fatal(e)
	}
	provider, e := semanticreview.NewRetouchPreviewProvider(api, objects, bucket, lookup, func(ctx context.Context, o objectstore.Object, expiry time.Time) (string, error) {
		ttl := time.Until(expiry)
		if ttl <= 0 || ttl > 14*time.Minute {
			return "", semanticreview.ErrInvalid
		}
		signed, e := objects.SignRead(ctx, o, ttl+time.Second)
		if e != nil {
			return "", e
		}
		if signed.ExpiresAt.Before(expiry) {
			return "", semanticreview.ErrInvalid
		}
		return signed.URL, nil
	}, lock, f.SRGB)
	if e != nil {
		t.Fatal(e)
	}
	icc, e := os.ReadFile("../../../resources/icc/work_rec2020_gamma24_v1.icc")
	if e != nil {
		t.Fatal(e)
	}
	color, e := semanticreview.NewExposureColorProfile(icc)
	if e != nil {
		t.Fatal(e)
	}
	apply, e := semanticreview.InstallExposureApplyService(ctx, semanticreview.ExposureApplyInstallation{API: api, Admission: admission, Objects: objects, Bucket: bucket, Color: color, Execution: entry.Execution, ReviewModelSHA: entry.Review.ModelSHA, ReviewDatasetSHA: entry.Review.DatasetSHA, DocumentBudgetBytes: entry.Review.DocumentBudgetBytes, ShownRetouchExecution: entry.ShownRetouchExecution, ShownRetouchPreview: provider})
	if e != nil {
		t.Fatal(e)
	}
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	authentication := &auth.Service{DB: authDB, Pepper: make([]byte, 32), Signer: auth.TokenSigner{Active: "fixture", Private: priv, Public: map[string]ed25519.PublicKey{"fixture": pub}, Issuer: "fixture", Audience: "fixture"}}
	// Auth epoch is read from the real current actor; no fabricated principal.
	cfg, e := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	owner, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.Close()
	var epoch int64
	if e = owner.QueryRow(ctx, "SELECT auth_epoch FROM photo.users WHERE id=$1", f.Actor).Scan(&epoch); e != nil {
		t.Fatal(e)
	}
	token, e := authentication.Signer.Issue(f.Actor, f.Session, epoch)
	if e != nil {
		t.Fatal(e)
	}
	handler := (&Server{DB: api, Auth: authentication, SemanticReviewPreview: provider, SemanticReviewApply: apply, Config: config.Config{Environment: "development", AppOrigin: "http://localhost:8080", PortalOrigin: "http://localhost:3000", MaxJSONBytes: 65536}}).Routes()
	call := func(method, path, key string, body []byte, access string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body)).WithContext(ctx)
		r.Header.Set("X-Photo-Client", "native")
		r.Header.Set("Content-Type", "application/json")
		if access != "" {
			r.Header.Set("Authorization", "Bearer "+access)
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	base := "/api/v1/tenants/" + f.Tenant.String() + "/semantic-intents/" + f.Intent.String()
	query := url.Values{"expected_revision": {f.Fence.ExpectedRevision}, "target_revision": {f.Fence.TargetRevision}, "plan_sha": {f.Fence.PlanSHA}, "candidate_sha": {f.Fence.CandidateSHA}}
	previewPath := base + "/targets/" + f.Target.String() + "/preview?" + query.Encode()
	if w := call("GET", previewPath, "", nil, ""); w.Code != 401 {
		t.Fatal("anonymous preview", w.Code)
	}
	w := call("GET", previewPath, "", nil, token)
	var preview semanticreview.Preview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.Validate(f.Tenant, f.Intent, f.Target, f.Fence, time.Now()) != nil {
		t.Fatal("real selected preview", w.Code, w.Body.String())
	}
	// Revision is canonical server input already validated above; assert no numeric rounding.
	if _, e := strconv.ParseInt(f.Fence.ExpectedRevision, 10, 64); e != nil {
		t.Fatal(e)
	}
	decision := semanticreview.ApplyDecision{PlanSHA: f.Fence.PlanSHA, ExpectedRevision: f.Fence.ExpectedRevision, TargetDecisions: []semanticreview.TargetDecision{{TargetID: f.Target, Decision: "approve", Preview: &semanticreview.PreviewSelection{Render: preview.Render, Evidence: preview.Evidence}}}}
	body, e := json.Marshal(decision)
	if e != nil {
		t.Fatal(e)
	}
	key := uuid.NewString()
	accepted := call("POST", base+"/apply", key, body, token)
	if accepted.Code != 200 {
		t.Fatal("real selected Apply", accepted.Code, accepted.Body.String())
	}
	replay := call("POST", base+"/apply", key, body, token)
	if replay.Code != 200 {
		t.Fatal("selected Apply replay", replay.Code, replay.Body.String())
	}
	var a, b map[string]any
	if json.Unmarshal(accepted.Body.Bytes(), &a) != nil || json.Unmarshal(replay.Body.Bytes(), &b) != nil {
		t.Fatal("Apply JSON")
	}
	// The real HTTP handler emits the immutable BatchResult directly.
	ar, _ := json.Marshal(a)
	br, _ := json.Marshal(b)
	if a["child"] == nil || !bytes.Equal(ar, br) {
		t.Fatal("immutable HTTP response changed")
	}
	output := os.Getenv("PHOTO_TEST_V27_APPLY_RESPONSE_FILE")
	if output == "" {
		t.Fatal("capture path required")
	}
	if e = os.WriteFile(output, accepted.Body.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	t.Log("V27_RETOUCH_SELECTED_HTTP_OK")
}
