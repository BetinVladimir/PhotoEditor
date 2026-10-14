package httpapi

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"strings"
	"testing"
)

type renderHTTPFreezer struct {
	calls         int
	who           auth.Identity
	tenant, asset uuid.UUID
	key           string
	request       v2render.RenderFreezeRequest
	err           error
	replay        bool
	mutate        func(*idempotency.Response)
}

func (f *renderHTTPFreezer) Freeze(_ context.Context, who auth.Identity, tenant, asset uuid.UUID, key string, request v2render.RenderFreezeRequest) (idempotency.Response, error) {
	f.calls++
	f.who = who
	f.tenant = tenant
	f.asset = asset
	f.key = key
	f.request = request
	id, err := v2render.RenderFreezeDocumentID(tenant, who.UserID, asset, key, request)
	if err != nil {
		return idempotency.Response{}, err
	}
	body, _ := canonical.Marshal(v2render.RenderFreezeResult{DocumentID: id, DraftDocumentID: request.DocumentID, DraftRevision: request.ExpectedRevision, Source: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}, Recipe: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}})
	result := idempotency.Response{Status: 200, Body: body, ResourceID: &id, Replayed: f.replay}
	if f.mutate != nil {
		f.mutate(&result)
	}
	return result, f.err
}

func TestIntegrationRenderFreezeHTTP(t *testing.T) {
	freezer := &renderHTTPFreezer{}
	handler, db, token, tenant := httpFixtureWithRenderFreezer(t, nil, nil, freezer)
	asset := uuid.New()
	path := "/api/v1/tenants/" + tenant.String() + "/assets/" + asset.String() + "/v2-draft/freeze"
	intent := v2render.RenderFreezeRequest{DocumentID: uuid.New(), ExpectedRevision: "4"}
	raw, _ := canonical.Marshal(intent)
	body := string(raw)
	response := request(handler, "POST", path, body, token, "freeze-http", "")
	if response.Code != 200 || freezer.calls != 1 || freezer.tenant != tenant || freezer.asset != asset || freezer.key != "freeze-http" || freezer.request != intent {
		t.Fatal("valid request", response.Code, response.Body.String())
	}
	var actor uuid.UUID
	if err := db.QueryRow(context.Background(), "SELECT owner_user_id FROM photo.tenants WHERE id=$1", tenant).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if freezer.who.UserID != actor || freezer.who.SessionID == uuid.Nil {
		t.Fatal("identity not forwarded")
	}
	id, _ := v2render.RenderFreezeDocumentID(tenant, actor, asset, "freeze-http", intent)
	if _, err := v2render.DecodeRenderFreezeResult(response.Body.Bytes(), id, intent); err != nil {
		t.Fatal("response changed", err)
	}
	invalid := []string{`{}`, strings.Replace(body, `"document_id"`, `"Document_ID"`, 1), strings.Replace(body, `"expected_revision":"4"`, `"expected_revision":4`, 1), strings.Replace(body, `"expected_revision":"4"`, `"expected_revision":"04"`, 1), strings.Replace(body, `"assume_profile":null`, `"assume_profile":"unknown"`, 1), strings.Replace(body, `"document_id":`, `"model_lock_sha":"`+strings.Repeat("d", 64)+`","document_id":`, 1), strings.Replace(body, `"document_id":`, `"assume_profile":null,"document_id":`, 1), body + `{}`}
	for _, bad := range invalid {
		count := freezer.calls
		response := request(handler, "POST", path, bad, token, "freeze-http-invalid", "")
		if (response.Code != 422 && response.Code != 400) || freezer.calls != count {
			t.Fatal("invalid input reached service", response.Code, bad)
		}
	}
	count := freezer.calls
	response = request(handler, "POST", path, body, token, "", "")
	if response.Code != 422 || freezer.calls != count {
		t.Fatal("missing key reached service", response.Code)
	}
	freezer.replay = true
	response = request(handler, "POST", path, body, token, "freeze-http", "")
	if response.Code != 200 || response.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("replay header", response.Code)
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{v2input.ErrInvalid, 422}, {v2input.ErrConflict, 409}, {v2input.ErrExpired, 410}, {objectstore.ErrUnavailable, 503}, {objectstore.ErrMissing, 503}, {objectstore.ErrIntegrity, 503}} {
		freezer.err = tc.err
		response := request(handler, "POST", path, body, token, "freeze-http", "")
		if response.Code != tc.status {
			t.Fatal("error mapping", response.Code)
		}
	}
	freezer.err = nil
	for _, mutate := range []func(*idempotency.Response){func(r *idempotency.Response) { r.Status = 202 }, func(r *idempotency.Response) { r.ResourceID = nil }, func(r *idempotency.Response) { id := uuid.New(); r.ResourceID = &id }, func(r *idempotency.Response) {
		var fields map[string]any
		json.Unmarshal(r.Body, &fields)
		fields["draft_revision"] = "5"
		r.Body, _ = canonical.Marshal(fields)
	}} {
		freezer.mutate = mutate
		response := request(handler, "POST", path, body, token, "freeze-http", "")
		if response.Code != 503 {
			t.Fatal("invalid service result accepted", response.Code)
		}
	}
}
