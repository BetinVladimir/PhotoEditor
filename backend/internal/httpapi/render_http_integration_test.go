package httpapi

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"reflect"
	"strings"
	"testing"
	"time"
)

type renderHTTPQueue struct {
	calls           int
	who             auth.Identity
	tenant, project uuid.UUID
	key             string
	intent          v2render.RenderBatchSubmissionRequest
	err             error
	mutate          func(*v2render.RenderBatchTicket)
}

func (q *renderHTTPQueue) Queue(_ context.Context, who auth.Identity, tenant, project uuid.UUID, key string, intent v2render.RenderBatchSubmissionRequest) (v2render.RenderBatchTicket, error) {
	q.calls++
	q.who = who
	q.tenant = tenant
	q.project = project
	q.key = key
	q.intent = intent
	batch, _, err := v2render.CompileRenderSubmissionIdentity(tenant, who.UserID, project, key, intent)
	if err != nil {
		return v2render.RenderBatchTicket{}, err
	}
	ticket := v2render.RenderBatchTicket{RunID: batch, State: "queued", ChildRunIDs: []uuid.UUID{uuid.NewSHA1(batch, []byte("render.batch:0"))}}
	if q.mutate != nil {
		q.mutate(&ticket)
	}
	return ticket, q.err
}
func renderHTTPIntent(count int) v2render.RenderBatchSubmissionRequest {
	intent := v2render.RenderBatchSubmissionRequest{SnapshotID: uuid.New(), PolicySHA: strings.Repeat("a", 64)}
	for i := 0; i < count; i++ {
		intent.Selection = append(intent.Selection, v2render.RenderSelectionItem{Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), CanvasHash: strings.Repeat("b", 64), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}}, Recipe: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}})
	}
	return intent
}
func TestIntegrationRenderHTTP(t *testing.T) {
	queue := &renderHTTPQueue{}
	h, db, token, tenant := httpFixtureWithQueues(t, nil, queue)
	project := uuid.New()
	path := "/api/v1/tenants/" + tenant.String() + "/projects/" + project.String() + "/render-batches"
	intent := renderHTTPIntent(2)
	raw, _ := json.Marshal(intent)
	body := string(raw)
	response := request(h, "POST", path, body, token, "render-http-test", "")
	if response.Code != 202 || queue.calls != 1 || queue.tenant != tenant || queue.project != project || queue.key != "render-http-test" || !reflect.DeepEqual(queue.intent, intent) {
		t.Fatal("valid request", response.Code, response.Body.String())
	}
	var actor uuid.UUID
	if err := db.QueryRow(context.Background(), "SELECT owner_user_id FROM photo.tenants WHERE id=$1", tenant).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if queue.who.UserID != actor || queue.who.SessionID == uuid.Nil {
		t.Fatal("identity not forwarded")
	}
	var ticket v2render.RenderBatchTicket
	batch, _, _ := v2render.CompileRenderSubmissionIdentity(tenant, actor, project, "render-http-test", intent)
	if json.Unmarshal(response.Body.Bytes(), &ticket) != nil || v2render.ValidateRenderBatchTicket(batch, ticket) != nil {
		t.Fatal("ticket altered")
	}
	invalid := []string{
		strings.Replace(body, `"policy_sha":`, `"POLICY_SHA":`, 1),
		strings.Replace(body, `"canvas_hash":`, `"Canvas_Hash":`, 1),
		strings.Replace(body, `"recipe":`, `"RECIPE":`, 1),
		strings.Replace(body, `"selection":`, `"runtime":"cuda","selection":`, 1),
		strings.Replace(body, `"selection":`, `"policy_sha":"`+strings.Repeat("a", 64)+`","selection":`, 1),
		strings.Replace(body, `"source":{"asset_version_id":`, `"source":{"extra":null,"asset_version_id":`, 1),
		`{"input_snapshot_id":"` + intent.SnapshotID.String() + `","policy_sha":"` + intent.PolicySHA + `","selection":null}`,
	}
	duplicate := intent
	duplicate.Selection = []v2render.RenderSelectionItem{intent.Selection[0], intent.Selection[0]}
	encoded, _ := json.Marshal(duplicate)
	invalid = append(invalid, string(encoded))
	for _, bad := range invalid {
		response = request(h, "POST", path, bad, token, "render-http-test", "")
		if response.Code < 400 || queue.calls != 1 {
			t.Fatal("invalid request reached service", response.Code, response.Body.String())
		}
	}
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{v2input.ErrConflict, 409, "IDEMPOTENCY_CONFLICT"}, {v2input.ErrExpired, 410, "SNAPSHOT_EXPIRED"}, {v2input.ErrInvalid, 422, "VALIDATION_FAILED"}, {domain.NewError("PROFILE_CONSENT_REVOKED", 409), 409, "PROFILE_CONSENT_REVOKED"}, {domain.NewError("RENDER_UNAVAILABLE", 503), 503, "RENDER_UNAVAILABLE"}, {domain.NewError("FORBIDDEN", 403), 403, "FORBIDDEN"},
	} {
		queue.err = c.err
		response = request(h, "POST", path, body, token, "render-http-test", "")
		if response.Code != c.status || !strings.Contains(response.Body.String(), c.code) {
			t.Fatal("error mapping", c.code, response.Code, response.Body.String())
		}
	}
	queue.err = nil
	for _, mutation := range []func(*v2render.RenderBatchTicket){func(t *v2render.RenderBatchTicket) { t.RunID = uuid.New() }, func(t *v2render.RenderBatchTicket) { t.State = "completed" }, func(t *v2render.RenderBatchTicket) { t.ChildRunIDs = nil }, func(t *v2render.RenderBatchTicket) { t.ChildRunIDs[0] = uuid.New() }} {
		queue.mutate = mutation
		response = request(h, "POST", path, body, token, "render-http-test", "")
		if response.Code != 503 {
			t.Fatal("invalid service response accepted", response.Code)
		}
	}
	queue.mutate = func(t *v2render.RenderBatchTicket) { t.State = "running" }
	response = request(h, "POST", path, body, token, "render-http-test", "")
	if response.Code != 202 {
		t.Fatal("saved running admission ticket rejected", response.Code)
	}
	queue.mutate = nil
	full := renderHTTPIntent(5000)
	encoded, _ = json.Marshal(full)
	if len(encoded) <= 1048576 || len(encoded) > v2render.MaxRenderSubmissionBytes {
		t.Fatal("large fixture does not exercise body limit", len(encoded))
	}
	response = request(h, "POST", path, string(encoded), token, "render-http-large", "")
	if response.Code != 202 || len(queue.intent.Selection) != 5000 {
		t.Fatal("full supported selection truncated", response.Code, response.Body.String())
	}
	before := queue.calls
	response = request(h, "POST", path, strings.Repeat(" ", v2render.MaxRenderSubmissionBytes+1), token, "render-http-limit", "")
	if response.Code != 413 || queue.calls != before {
		t.Fatal("oversize request reached service", response.Code)
	}
}
func TestIntegrationRenderHTTPRejectsBeforeQueue(t *testing.T) {
	queue := &renderHTTPQueue{}
	h, _, token, tenant := httpFixtureWithQueues(t, nil, queue)
	path := "/api/v1/tenants/" + tenant.String() + "/projects/" + uuid.NewString() + "/render-batches"
	raw, _ := json.Marshal(renderHTTPIntent(1))
	for _, c := range []struct {
		token, key string
		status     int
	}{{"", "render-http", 401}, {token, "", 422}, {token, strings.Repeat("x", 300), 422}} {
		response := request(h, "POST", path, string(raw), c.token, c.key, "")
		if response.Code != c.status || queue.calls != 0 {
			t.Fatal("invalid credentials/key reached service", response.Code, queue.calls)
		}
	}
	web := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
	web.Header.Set("Authorization", "Bearer "+token)
	web.Header.Set("Content-Type", "application/json")
	web.Header.Set("Origin", "http://localhost:8080")
	web.Header.Set("Idempotency-Key", "render-http-web")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, web)
	if response.Code != 403 || queue.calls != 0 {
		t.Fatal("web mutation without CSRF accepted", response.Code, response.Body.String())
	}
	h, _, token, tenant = httpFixtureWithQueues(t, nil, nil)
	path = "/api/v1/tenants/" + tenant.String() + "/projects/" + uuid.NewString() + "/render-batches"
	response = request(h, "POST", path, string(raw), token, "render-http-disabled", "")
	if response.Code != 503 {
		t.Fatal("absent render service accepted", response.Code)
	}
}

func TestIntegrationRenderHTTPRealAdmissionGate(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB required")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("fixture configuration unavailable")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	cfg.MaxConns = 4
	scheduler, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	// Missing calibration must fail before using unavailable source dependencies.
	service := v2render.SQLRenderSubmissionService{Factory: v2render.SQLRenderBatchFactory{Scheduler: scheduler}, Timeout: 2 * time.Second}
	h, admin, token, tenant := httpFixtureWithQueues(t, nil, service)
	base := "/api/v1/tenants/" + tenant.String() + "/projects"
	created := request(h, "POST", base, `{"name":"Render gate","description":"fixture","visibility":"restricted","download_allowed":true}`, token, "render-gate-project", "")
	var project struct {
		ID uuid.UUID `json:"id"`
	}
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &project) != nil || project.ID == uuid.Nil {
		t.Fatal("project fixture", created.Code, created.Body.String())
	}
	intent := renderHTTPIntent(1)
	// A well-formed but stale policy must be rejected before calibration lookup.
	raw, _ := json.Marshal(intent)
	response := request(h, "POST", base+"/"+project.ID.String()+"/render-batches", string(raw), token, "render-gate-stale-policy", "")
	if response.Code != 422 || !strings.Contains(response.Body.String(), "VALIDATION_FAILED") {
		t.Fatal("stale policy bypassed admission validation", response.Code, response.Body.String())
	}
	policy, err := v2render.DefaultRenderPolicy()
	if err != nil {
		t.Fatal("current render policy unavailable", err)
	}
	intent.PolicySHA = policy.PolicySHA
	raw, _ = json.Marshal(intent)
	response = request(h, "POST", base+"/"+project.ID.String()+"/render-batches", string(raw), token, "render-gate-submit", "")
	if response.Code != 503 || !strings.Contains(response.Body.String(), "RENDER_UNAVAILABLE") {
		t.Fatal("fresh uncalibrated admission bypassed gate", response.Code, response.Body.String())
	}
	response = request(h, "POST", base+"/"+uuid.NewString()+"/render-batches", string(raw), token, "render-gate-submit", "")
	if response.Code != 410 {
		t.Fatal("foreign project crossed scoped probe", response.Code, response.Body.String())
	}
	response = request(h, "POST", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+project.ID.String()+"/render-batches", string(raw), token, "render-gate-submit", "")
	if response.Code != 410 {
		t.Fatal("foreign tenant crossed scoped probe", response.Code, response.Body.String())
	}
	var runs, keys int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM photo.runs WHERE tenant_id=$1),(SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND route='V2.Admit')`, tenant).Scan(&runs, &keys); err != nil || runs != 0 || keys != 0 {
		t.Fatal("refused HTTP admission left persistent work", err, runs, keys)
	}
}
