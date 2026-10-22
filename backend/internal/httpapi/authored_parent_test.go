package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	runservice "photoeditor/backend/internal/run"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type parentTransportRecorder struct {
	begin                   int
	read                    int
	who                     auth.Identity
	tenant, project, parent uuid.UUID
	key, sha                string
	req                     runservice.EditorEffectRequest
	err                     error
}

func (p *parentTransportRecorder) BeginAuthoredRender(_ context.Context, w auth.Identity, t, project uuid.UUID, key string, req runservice.EditorEffectRequest) (idempotency.Response, error) {
	p.begin++
	p.who = w
	p.tenant = t
	p.project = project
	p.key = key
	p.req = req
	return idempotency.Response{Status: 202, Body: json.RawMessage(`{"run_id":"` + p.parent.String() + `","status":"running"}`), Replayed: true}, p.err
}
func (p *parentTransportRecorder) ReadAuthoredParent(_ context.Context, w auth.Identity, t, project, parent uuid.UUID, sha string) (runservice.AuthoredParentLink, error) {
	p.read++
	p.who = w
	p.tenant = t
	p.project = project
	p.sha = sha
	return runservice.AuthoredParentLink{SchemaVersion: 1, Tenant: t, Project: project, Parent: parent, RequestSHA: sha, Admission: "accepted"}, p.err
}
func parentRouteFixture(t *testing.T) (*chi.Mux, *parentTransportRecorder, auth.Identity, uuid.UUID, uuid.UUID) {
	t.Helper()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenantID, projectID := uuid.New(), uuid.New()
	p := &parentTransportRecorder{parent: uuid.New()}
	r := chi.NewRouter()
	RegisterAuthoredParentRoutes(r, func(_ *http.Request, mutate bool) (auth.Identity, uuid.UUID, error) { return who, tenantID, nil }, p)
	return r, p, who, tenantID, projectID
}
func TestAuthoredParentActualTransportRequestAndReplayLink(t *testing.T) {
	r, p, who, tenantID, projectID := parentRouteFixture(t)
	pin := strings.Repeat("a", 64)
	body := runservice.EditorEffectRequest{Source: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: pin}, CanvasHash: pin}, Recipe: v2input.Ref{ArtifactID: uuid.New(), SHA256: pin}, FrozenDocumentID: uuid.New(), DraftDocumentID: uuid.New(), DraftRevision: 1}
	raw, _ := json.Marshal(body)
	base := "/api/v1/tenants/" + tenantID.String() + "/projects/" + projectID.String() + "/authored-renders"
	req := httptest.NewRequest("POST", base, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "actual-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 202 || p.begin != 1 || p.who != who || p.tenant != tenantID || p.project != projectID || p.req != body || p.key != "actual-key" || w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal(w.Code, p)
	}
	var accepted struct {
		RunID  uuid.UUID `json:"run_id"`
		Status string    `json:"status"`
	}
	if json.Unmarshal(w.Body.Bytes(), &accepted) != nil || accepted.RunID != p.parent || accepted.Status != "running" {
		t.Fatal("actual receipt changed")
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", base+"/"+accepted.RunID.String()+"?request_sha256="+pin, nil))
	if w.Code != 200 || p.read != 1 || p.sha != pin || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Code, p)
	}
}
func TestAuthoredParentRouteRejectsOverridesAndAmbiguousReadback(t *testing.T) {
	r, p, _, tenantID, projectID := parentRouteFixture(t)
	base := "/api/v1/tenants/" + tenantID.String() + "/projects/" + projectID.String() + "/authored-renders"
	for _, q := range []string{"", "?request_sha256=bad", "?request_sha256=" + strings.Repeat("a", 64) + "&request_sha256=" + strings.Repeat("b", 64), "?request_sha256=" + strings.Repeat("a", 64) + "&source=fake"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", base+"/"+p.parent.String()+q, nil))
		if w.Code != 422 {
			t.Fatal(w.Code, q)
		}
	}
	for _, body := range []string{`{"source_override":"fake"}`, `{} {}`} {
		req := httptest.NewRequest("POST", base, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "key")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := 422
		if body == `{} {}` {
			want = 400
		}
		if w.Code != want {
			t.Fatal(w.Code)
		}
	}
	if p.begin != 0 || p.read != 0 {
		t.Fatal("invalid request reached service")
	}
}
func TestAuthoredParentWithdrawalAndMissingServiceFailClosed(t *testing.T) {
	r := chi.NewRouter()
	p := &parentTransportRecorder{}
	RegisterAuthoredParentRoutes(r, func(*http.Request, bool) (auth.Identity, uuid.UUID, error) {
		return auth.Identity{}, uuid.Nil, domain.NewError("FORBIDDEN", 403)
	}, p)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/authored-renders", strings.NewReader(`{}`)))
	if w.Code != 403 || p.begin != 0 {
		t.Fatal(w.Code)
	}
}
func TestAuthoredParentRealServerMountRefusesUnauthenticated(t *testing.T) {
	handler := (&Server{Auth: &auth.Service{}}).Routes()
	base := "/api/v1/tenants/" + uuid.NewString() + "/projects/" + uuid.NewString() + "/authored-renders"
	for _, target := range []struct{ method, path string }{{"POST", base}, {"GET", base + "/" + uuid.NewString() + "?request_sha256=" + strings.Repeat("a", 64)}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(target.method, target.path, nil))
		if w.Code != 401 {
			t.Fatal("mounted endpoint did not authenticate", w.Code)
		}
	}
}
func TestAuthoredParentMissingServiceAndMutationBoundary(t *testing.T) {
	r := chi.NewRouter()
	mutations := []bool{}
	RegisterAuthoredParentRoutes(r, func(_ *http.Request, mutate bool) (auth.Identity, uuid.UUID, error) {
		mutations = append(mutations, mutate)
		return auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), nil
	}, nil)
	base := "/api/v1/tenants/" + uuid.NewString() + "/projects/" + uuid.NewString() + "/authored-renders"
	req := httptest.NewRequest("POST", base, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "valid")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", base+"/"+uuid.NewString()+"?request_sha256="+strings.Repeat("a", 64), nil))
	if w.Code != 503 || len(mutations) != 2 || !mutations[0] || mutations[1] {
		t.Fatal(w.Code, mutations)
	}
}
