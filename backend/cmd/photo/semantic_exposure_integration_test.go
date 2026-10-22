package main

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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticreview"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Only invoked by the owned PostgreSQL/MinIO fixture. Credentials stay in memory
// and sanitized failures never print DSNs, passwords or generated role SQL.
func TestIntegrationSemanticExposureBootstrap(t *testing.T) {
	dsn, run := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_TEST_V27_ASSEMBLY_RUN")
	if dsn == "" || run == "" {
		t.Skip("isolated fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	owner, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("fixture database unavailable")
	}
	t.Cleanup(owner.Close)
	var tenant uuid.UUID
	if err = owner.QueryRow(ctx, "SELECT tenant_id FROM photo.runs WHERE id=$1", run).Scan(&tenant); err != nil {
		t.Fatal("fixture run unavailable")
	}
	role := "photo_test_admission_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	password := strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{role}.Sanitize()
	if _, err = owner.Exec(ctx, "CREATE ROLE "+identifier+" LOGIN NOINHERIT NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION PASSWORD '"+password+"'"); err != nil {
		t.Fatal("temporary login unavailable")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := owner.Exec(cleanup, "DROP ROLE "+identifier); err != nil {
			t.Error("temporary login cleanup failed")
		}
	})
	if _, err = owner.Exec(ctx, "GRANT photo_semantic_admission TO "+identifier); err != nil {
		t.Fatal("temporary membership unavailable")
	}
	loginConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("fixture configuration invalid")
	}
	loginConfig.ConnConfig.User = role
	loginConfig.ConnConfig.Password = password
	delete(loginConfig.ConnConfig.RuntimeParams, "role")
	apiConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("fixture API configuration invalid")
	}
	apiConfig.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, apiConfig)
	if err != nil {
		t.Fatal("fixture API unavailable")
	}
	t.Cleanup(api.Close)
	store, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET"), AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal("fixture storage unavailable")
	}
	var pin semanticreview.ExposureQualificationPin
	if json.Unmarshal([]byte(os.Getenv("PHOTO_TEST_V27_EXPOSURE_PIN")), &pin) != nil {
		t.Fatal("fixture qualification invalid")
	}
	catalog := semanticreview.ExposurePreparationCatalog{SchemaVersion: 1, Tenants: []semanticreview.ExposureTenantPreparation{{TenantID: tenant, Review: semanticreview.ReviewPreparationConfig{SchemaVersion: 1, ModelSHA: strings.Repeat("a", 64), DatasetSHA: strings.Repeat("d", 64), DocumentBudgetBytes: 64 << 20}, Execution: semanticreview.ExposureDeploymentBudget{Qualification: pin, OwnerCosts: semanticreview.ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20}, Limits: semanticreview.ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}, ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA}}}}
	if os.Getenv("PHOTO_TEST_V27_SHOWN_PHASE") != "" {
		var shown semanticreview.ExposureQualificationPin
		if json.Unmarshal([]byte(os.Getenv("PHOTO_TEST_V27_SHOWN_PIN")), &shown) != nil {
			t.Fatal("shown qualification invalid")
		}
		budget := catalog.Tenants[0].Execution
		budget.Qualification = shown
		catalog.Tenants[0].ShownExecution = &budget
	}
	raw, _ := json.Marshal(catalog)
	sum := sha256.Sum256(raw)
	path := filepath.Join(t.TempDir(), "exposure.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal("fixture catalog unavailable")
	}
	icc, err := filepath.Abs("../../../resources/icc")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PHOTO_SEMANTIC_EXPOSURE_CONFIG_FILE", path)
	t.Setenv("PHOTO_SEMANTIC_EXPOSURE_CONFIG_SHA256", hex.EncodeToString(sum[:]))
	t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_FILE", "")
	t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_SHA256", "")
	t.Setenv("PHOTO_ICC_ROOT", icc)
	loginURL := url.URL{Scheme: "postgres", User: url.UserPassword(role, password), Host: net.JoinHostPort(loginConfig.ConnConfig.Host, strconv.Itoa(int(loginConfig.ConnConfig.Port))), Path: "/" + loginConfig.ConnConfig.Database, RawQuery: "sslmode=disable"}
	t.Setenv("PHOTO_SEMANTIC_ADMISSION_DATABASE_URL", loginURL.String())
	var preview *semanticreview.ExposurePreviewProvider
	if os.Getenv("PHOTO_TEST_V27_SHOWN_PHASE") != "" {
		pins, e := colorprofile.LoadSHAs(icc)
		if e != nil {
			t.Fatal(e)
		}
		preview, e = buildSemanticPreview(api, store, os.Getenv("PHOTO_TEST_MINIO_BUCKET"), pins["srgb_v1"])
		if e != nil {
			t.Fatal(e)
		}
	}
	service, closeFn, err := buildSemanticExposureApply(ctx, api, store, os.Getenv("PHOTO_TEST_MINIO_BUCKET"), preview)
	if err != nil || service == nil {
		t.Fatal("dedicated login bootstrap failed", err)
	}
	t.Cleanup(closeFn)
	if handoff := os.Getenv("PHOTO_TEST_V27_PREPARED_APPLY_FILE"); handoff != "" {
		var prepared struct {
			Tenant, Intent, Actor, Session uuid.UUID
			Key                            string
			Request                        semanticreview.ApplyDecision
		}
		raw, err := os.ReadFile(handoff)
		if err != nil || len(raw) > 65536 || json.Unmarshal(raw, &prepared) != nil || prepared.Tenant != tenant {
			t.Fatal("prepared fixture handoff unavailable")
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal("fixture signing key unavailable")
		}
		authConfig, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			t.Fatal("fixture auth configuration unavailable")
		}
		authConfig.ConnConfig.RuntimeParams["role"] = "photo_auth"
		authPool, err := pgxpool.NewWithConfig(ctx, authConfig)
		if err != nil {
			t.Fatal("fixture auth pool unavailable")
		}
		t.Cleanup(authPool.Close)
		authService := &auth.Service{DB: authPool, Pepper: make([]byte, 32), Signer: auth.TokenSigner{Active: "fixture", Private: priv, Public: map[string]ed25519.PublicKey{"fixture": pub}, Issuer: "fixture", Audience: "fixture"}}
		var epoch int64
		if err = owner.QueryRow(ctx, "SELECT auth_epoch FROM photo.users WHERE id=$1", prepared.Actor).Scan(&epoch); err != nil {
			t.Fatal("fixture actor unavailable")
		}
		token, err := authService.Signer.Issue(prepared.Actor, prepared.Session, epoch)
		if err != nil {
			t.Fatal("fixture access token unavailable")
		}
		var capturedReviewPreview json.RawMessage
		if shownCapturePhase(t, ctx, api, owner, store, os.Getenv("PHOTO_TEST_MINIO_BUCKET"), authService, token,
			auth.Identity{UserID: prepared.Actor, SessionID: prepared.Session}, tenant, prepared.Intent, &prepared.Request, &capturedReviewPreview, catalog, preview) {
			return
		}
		handler := (&httpapi.Server{DB: api, Auth: authService, SemanticReviewApply: service, Config: config.Config{Environment: "development", AppOrigin: "http://localhost:8080", PortalOrigin: "http://localhost:3000", MaxJSONBytes: 65536}}).Routes()
		requestBody, _ := json.Marshal(prepared.Request)
		path := "/api/v1/tenants/" + tenant.String() + "/semantic-intents/" + prepared.Intent.String() + "/apply"
		post := func(access, key string, web bool) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(requestBody)).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json")
			if web {
				r.Header.Set("Origin", "http://localhost:8080")
			} else {
				r.Header.Set("X-Photo-Client", "native")
			}
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
		if denied := post("", prepared.Key, false); denied.Code != 401 {
			t.Fatal("unauthenticated HTTP Apply accepted")
		}
		if denied := post(token, prepared.Key, true); denied.Code != 403 {
			t.Fatal("web Apply without CSRF accepted", denied.Code)
		}
		if denied := post(token, "", false); denied.Code != 400 {
			t.Fatal("HTTP Apply without key accepted", denied.Code)
		}
		var receiptCount int
		if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND route='SemanticIntent.Apply' AND key=$2", tenant, prepared.Key).Scan(&receiptCount); err != nil || receiptCount != 0 {
			t.Fatal("denied HTTP requests created receipt")
		}
		closeRead, err := semanticreview.NewPlanObjectReader(store, os.Getenv("PHOTO_TEST_MINIO_BUCKET"))
		if err != nil {
			t.Fatal(err)
		}
		reviewConfig := catalog.Tenants[0].Review
		prepareClose, err := semanticreview.NewClosePreparation(api, store, closeRead, os.Getenv("PHOTO_TEST_MINIO_BUCKET"), reviewConfig.DocumentBudgetBytes, reviewConfig.ModelSHA, reviewConfig.DatasetSHA)
		if err != nil {
			t.Fatal(err)
		}
		closeRequest := semanticreview.ReviewClose{ExpectedRevision: prepared.Request.ExpectedRevision, PlanSHA: prepared.Request.PlanSHA}
		if _, err := prepareClose(ctx, auth.Identity{UserID: prepared.Actor, SessionID: prepared.Session}, tenant, prepared.Intent, closeRequest); !errors.Is(err, semanticreview.ErrReviewIncomplete) {
			t.Fatal("undecided review closure not denied", err)
		}
		getReview := func(suffix string) *httptest.ResponseRecorder {
			r := httptest.NewRequest("GET", strings.TrimSuffix(path, "/apply")+suffix, nil).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("X-Photo-Client", "native")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			return w
		}
		proposedPage, reviewStatus := getReview("/targets"), getReview("/review")
		if proposedPage.Code != 200 || reviewStatus.Code != 200 {
			t.Fatal("pre-Apply review projection unavailable", proposedPage.Code, reviewStatus.Code)
		}
		response := post(token, prepared.Key, false)
		if response.Code != 200 {
			t.Fatal("fresh HTTP bootstrapped Apply failed", response.Code)
		}
		result, err := semanticreview.DecodeBatchResult(response.Body.Bytes())
		if err != nil || result.Child == nil {
			t.Fatal("fresh HTTP child missing")
		}
		if capture := os.Getenv("PHOTO_TEST_V27_APPLY_RESPONSE_FILE"); capture != "" {
			raw, err := json.Marshal(struct {
				Tenant   uuid.UUID                    `json:"tenant"`
				Intent   uuid.UUID                    `json:"intent"`
				Key      string                       `json:"key"`
				Request  semanticreview.ApplyDecision `json:"request"`
				Response json.RawMessage              `json:"response"`
				Targets  json.RawMessage              `json:"targets"`
				Status   json.RawMessage              `json:"status"`
				Preview  json.RawMessage              `json:"preview,omitempty"`
			}{tenant, prepared.Intent, prepared.Key, prepared.Request, response.Body.Bytes(), proposedPage.Body.Bytes(), reviewStatus.Body.Bytes(), capturedReviewPreview})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(capture, raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if os.Getenv("PHOTO_TEST_V27_SHOWN_PHASE") == "apply" {
			t.Log("V27_SHOWN_APPLY_QUEUED")
			return
		}
		var sealed bool
		if err = owner.QueryRow(ctx, "SELECT graph_sealed AND status='queued' AND (SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND state='ready')=1 FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, result.Child.RunID).Scan(&sealed); err != nil || !sealed {
			t.Fatal("fresh bootstrapped child not ready")
		}
		closeRequest.ExpectedRevision = result.Revision
		closePrepared, err := prepareClose(ctx, auth.Identity{UserID: prepared.Actor, SessionID: prepared.Session}, tenant, prepared.Intent, closeRequest)
		if err != nil {
			t.Fatal("queued child close preparation failed", err)
		}
		closeTX, err := api.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeTX.Rollback(context.Background()) })
		closeResponse, err := semanticreview.CommitClose(ctx, closeTX, tenant, prepared.Intent, prepared.Actor, prepared.Session, "queued-child-rollback-close", closePrepared.Review, closePrepared.Targets, closeRequest, closePrepared.Decision)
		if err != nil {
			t.Fatal("queued child blocked parent closure", err)
		}
		closed, err := semanticreview.DecodeCloseResult(closeResponse.Body)
		if err != nil {
			t.Fatal(err)
		}
		var preserved bool
		if err := closeTX.QueryRow(ctx, `SELECT r.status='queued' AND NOT r.cancel_requested AND r.graph_sealed AND (SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND state='ready')=1 AND (SELECT count(*) FROM photo.semantic_targets WHERE tenant_id=$1 AND child_run_id=$2 AND state='queued')=1 FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$2`, tenant, result.Child.RunID).Scan(&preserved); err != nil || !preserved {
			t.Fatal("closure changed admitted child", err)
		}
		var parentClosed bool
		if err := closeTX.QueryRow(ctx, `SELECT status='completed' AND revision::text=$3 FROM photo.runs WHERE tenant_id=$1 AND id=$2`, tenant, closed.RunID, closed.Revision).Scan(&parentClosed); err != nil || !parentClosed {
			t.Fatal("parent closure not visible", err)
		}
		if err := closeTX.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var restored bool
		if err := owner.QueryRow(ctx, `SELECT status='waiting_review' AND revision::text=$3 AND NOT EXISTS(SELECT 1 FROM photo.v2_review_batches WHERE tenant_id=$1 AND workflow_id=$4 AND action='close') AND NOT EXISTS(SELECT 1 FROM photo.idempotency_records WHERE tenant_id=$1 AND route='SemanticIntent.CloseReview' AND key='queued-child-rollback-close') FROM photo.runs WHERE tenant_id=$1 AND id=$2`, tenant, closed.RunID, result.Revision, closed.WorkflowID).Scan(&restored); err != nil || !restored {
			t.Fatal("close rollback left admission state", err)
		}
		t.Log("V27_CLOSE_QUEUED_CHILD_OK")
		if os.Getenv("PHOTO_TEST_V27_EARLY_CLOSE") == "true" {
			closer, ok := service.(httpapi.SemanticReviewClose)
			if !ok {
				t.Fatal("tenant bootstrap missing close service")
			}
			durable, err := closer.Close(ctx, auth.Identity{UserID: prepared.Actor, SessionID: prepared.Session}, tenant, prepared.Intent, "durable-early-close", closeRequest)
			if err != nil {
				t.Fatal("durable early close failed", err)
			}
			closed, err := semanticreview.DecodeCloseResult(durable.Body)
			if err != nil {
				t.Fatal(err)
			}
			var persisted bool
			if err := owner.QueryRow(ctx, `SELECT parent.status='completed' AND parent.revision::text=$4 AND child.status='queued' AND NOT child.cancel_requested AND target.state='queued' FROM photo.runs parent JOIN photo.runs child ON child.tenant_id=parent.tenant_id AND child.id=$3 JOIN photo.semantic_targets target ON target.tenant_id=child.tenant_id AND target.child_run_id=child.id WHERE parent.tenant_id=$1 AND parent.id=$2`, tenant, closed.RunID, result.Child.RunID, closed.Revision).Scan(&persisted); err != nil || !persisted {
				t.Fatal("early closure not durable or changed child", err)
			}
			t.Log("V27_CLOSE_EARLY_DURABLE_OK")
		}
		replay := post(token, prepared.Key, false)
		firstBody, _ := canonical.Bytes(response.Body.Bytes())
		replayBody, _ := canonical.Bytes(replay.Body.Bytes())
		if replay.Code != 200 || !bytes.Equal(firstBody, replayBody) {
			t.Fatal("HTTP bootstrapped receipt replay failed")
		}
		if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND route='SemanticIntent.Apply' AND key=$2", tenant, prepared.Key).Scan(&receiptCount); err != nil || receiptCount != 1 {
			t.Fatal("HTTP receipt count changed")
		}
		t.Log("V27_EXPOSURE_HTTP_OK")
		t.Log("V27_EXPOSURE_FRESH_OK")
	}
	if os.Getenv("PHOTO_TEST_V27_BOOTSTRAP_REPLAY") == "true" {
		var intent, actor, session uuid.UUID
		var key string
		var expected json.RawMessage
		var source semanticreview.PlanSource
		err = owner.QueryRow(ctx, `SELECT i.id,b.actor_id,(r.config_snapshot->'v27_semantic'->>'auth_session_id')::uuid,
 receipt.key,receipt.response_body,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.v2_review_batches b JOIN photo.v2_workflows w ON w.tenant_id=b.tenant_id AND w.id=b.workflow_id
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 JOIN photo.semantic_intents i ON i.tenant_id=b.tenant_id AND i.workflow_id=b.workflow_id
 JOIN photo.idempotency_records receipt ON receipt.tenant_id=b.tenant_id AND receipt.resource_id=b.id AND receipt.route='SemanticIntent.Apply'
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.decisions_artifact_id
 WHERE r.id=$1 AND b.child_run_id IS NOT NULL`, run).Scan(&intent, &actor, &session, &key, &expected, &source.Ref.ArtifactID, &source.Ref.SHA256, &source.Bytes, &source.Bucket, &source.Key, &source.Version, &source.MediaType)
		if err != nil {
			t.Fatal("durable fixture receipt unavailable")
		}
		read, err := semanticreview.NewPlanObjectReader(store, source.Bucket)
		if err != nil {
			t.Fatal("fixture document reader unavailable")
		}
		body, err := read(ctx, source)
		if err != nil {
			t.Fatal("fixture decision document unavailable")
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != source.Ref.SHA256 {
			t.Fatal("fixture decision document digest mismatch")
		}
		var decision struct {
			ExpectedRevision string                          `json:"expected_revision"`
			Plan             v2input.Ref                     `json:"plan"`
			Targets          []semanticreview.TargetDecision `json:"targets"`
		}
		if json.Unmarshal(body, &decision) != nil {
			t.Fatal("fixture decision invalid")
		}
		request := semanticreview.ApplyDecision{PlanSHA: decision.Plan.SHA256, ExpectedRevision: decision.ExpectedRevision, TargetDecisions: decision.Targets}
		response, err := service.Apply(ctx, auth.Identity{UserID: actor, SessionID: session}, tenant, intent, key, request)
		if err != nil || !response.Replayed || response.Status != 200 {
			t.Fatal("bootstrapped durable Apply replay failed")
		}
		got, _ := canonical.Bytes(response.Body)
		want, _ := canonical.Bytes(expected)
		if !bytes.Equal(got, want) {
			t.Fatal("bootstrapped replay changed response")
		}
		t.Log("V27_EXPOSURE_REPLAY_OK")
	}
	t.Setenv("PHOTO_SEMANTIC_ADMISSION_DATABASE_URL", dsn)
	if _, closeUnsafe, err := buildSemanticExposureApply(ctx, api, store, os.Getenv("PHOTO_TEST_MINIO_BUCKET")); err == nil {
		closeUnsafe()
		t.Fatal("privileged fixture login accepted")
	}
	t.Log("V27_EXPOSURE_BOOTSTRAP_OK")
}
