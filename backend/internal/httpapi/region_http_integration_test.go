package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2region"
)

type regionHTTPQueue struct {
	calls         int
	who           auth.Identity
	tenant, asset uuid.UUID
	request       v2region.RegionPreviewRequest
	ticket        v2input.JobTicket
	err           error
}

func (q *regionHTTPQueue) Queue(_ context.Context, who auth.Identity, tenant, asset uuid.UUID, _ string, request v2region.RegionPreviewRequest) (v2input.JobTicket, error) {
	q.calls++
	q.who = who
	q.tenant = tenant
	q.asset = asset
	q.request = request
	return q.ticket, q.err
}

func TestIntegrationRegionHTTP(t *testing.T) {
	queue := &regionHTTPQueue{ticket: v2input.JobTicket{RunID: uuid.New(), WorkflowID: uuid.New(), State: "queued"}}
	h, db, token, tenant := httpFixtureWithRegion(t, queue)
	asset, version, recipe := uuid.New(), uuid.New(), uuid.New()
	path := "/api/v1/tenants/" + tenant.String() + "/assets/" + asset.String() + "/region-previews"
	body := `{"version_id":"` + version.String() + `","recipe":{"artifact_id":"` + recipe.String() + `","sha256":"` + strings.Repeat("a", 64) + `"},"rect":{"x0":0,"y0":0,"x1":1,"y1":1},"max_edge":1024}`
	w := request(h, "POST", path, body, token, "region-http-test", "")
	if w.Code != 202 || queue.calls != 1 || queue.tenant != tenant || queue.asset != asset || queue.request.VersionID != version {
		t.Fatal("valid region request", w.Code, w.Body.String())
	}
	var actor uuid.UUID
	if err := db.QueryRow(context.Background(), "SELECT owner_user_id FROM photo.tenants WHERE id=$1", tenant).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if queue.who.UserID != actor || queue.who.SessionID == uuid.Nil {
		t.Fatal("identity not forwarded")
	}
	var ticket v2input.JobTicket
	if json.Unmarshal(w.Body.Bytes(), &ticket) != nil || ticket != queue.ticket {
		t.Fatal("ticket not forwarded")
	}
	for _, invalid := range []string{strings.Replace(body, `"max_edge":1024`, `"max_edge":1024,"max_edge":512`, 1), strings.Replace(body, `"max_edge":1024`, `"max_edge":2048`, 1), strings.Replace(body, `"x0":0,`, "", 1), strings.Replace(body, `"max_edge":1024`, `"max_edge":1024,"runtime":"cuda"`, 1)} {
		w = request(h, "POST", path, invalid, token, "region-http-invalid", "")
		if w.Code < 400 || queue.calls != 1 {
			t.Fatal("invalid body reached queue", w.Code, w.Body.String())
		}
	}
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{v2input.ErrConflict, 409, "IDEMPOTENCY_CONFLICT"},
		{v2input.ErrExpired, 410, "SNAPSHOT_EXPIRED"},
		{v2region.ErrRegionInvalid, 422, "VALIDATION_FAILED"},
		{v2region.ErrRegionUnavailable, 503, "DEPENDENCY_UNAVAILABLE"},
	} {
		queue.err = c.err
		w = request(h, "POST", path, body, token, "region-http-test", "")
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.code) {
			t.Fatal("error mapping", c.code, w.Code, w.Body.String())
		}
	}
	queue.err = nil
	queue.ticket.State = "running"
	w = request(h, "POST", path, body, token, "region-http-test", "")
	if w.Code != 503 {
		t.Fatal("invalid queue ticket accepted", w.Code)
	}
}

func TestIntegrationRegionHTTPRejectsBeforeQueue(t *testing.T) {
	queue := &regionHTTPQueue{}
	h, _, token, tenant := httpFixtureWithRegion(t, queue)
	path := "/api/v1/tenants/" + tenant.String() + "/assets/" + uuid.NewString() + "/region-previews"
	body := `{"version_id":"` + uuid.NewString() + `","recipe":{"artifact_id":"` + uuid.NewString() + `","sha256":"` + strings.Repeat("a", 64) + `"},"rect":{"x0":0,"y0":0,"x1":1,"y1":1},"max_edge":1024}`
	for _, c := range []struct {
		token, key string
		status     int
	}{
		{"", "region-http", 401},
		{token, "", 422},
		{token, strings.Repeat("x", 300), 422},
	} {
		w := request(h, "POST", path, body, c.token, c.key, "")
		if w.Code != c.status || queue.calls != 0 {
			t.Fatal("rejected request reached queue", w.Code, w.Body.String(), queue.calls)
		}
	}
}
