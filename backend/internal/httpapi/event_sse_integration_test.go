package httpapi

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIntegrationEventSSE(t *testing.T) {
	h, db, token, tenant := httpFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var actor uuid.UUID
	if err := db.QueryRow(ctx, `SELECT owner_user_id FROM photo.tenants WHERE id=$1`, tenant).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES($1,0) ON CONFLICT(tenant_id) DO NOTHING`, tenant); err != nil {
		t.Fatal(err)
	}
	var cursor int64
	if err := db.QueryRow(ctx, `UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=$1 RETURNING last_cursor`, tenant).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO photo.run_events(tenant_id,cursor,principal_id,audience,type,payload) VALUES($1,$2,$3,'principal','capture.alert','{"session_id":"test"}')`, tenant, cursor, actor); err != nil {
		t.Fatal(err)
	}
	var other uuid.UUID
	if err := db.QueryRow(ctx, `SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`, uuid.NewString()+"@sse.test").Scan(&other); err != nil {
		t.Fatal(err)
	}
	var hiddenCursor int64
	if err := db.QueryRow(ctx, `UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=$1 RETURNING last_cursor`, tenant).Scan(&hiddenCursor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO photo.run_events(tenant_id,cursor,principal_id,audience,type,payload) VALUES($1,$2,$3,'principal','capture.alert','{"private":"other-user"}')`, tenant, hiddenCursor, other); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	open := func(after string) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/tenants/"+tenant.String()+"/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Photo-Client", "native")
		req.Header.Set("Accept", "text/event-stream")
		if after != "" {
			req.Header.Set("Last-Event-ID", after)
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	response := open("")
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		response.Body.Close()
		t.Fatal("SSE response", response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	var block strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		block.WriteString(line)
		if line == "\n" {
			break
		}
	}
	if !strings.Contains(block.String(), fmt.Sprintf("id: %d\n", cursor)) || !strings.Contains(block.String(), `"type":"capture.alert"`) {
		t.Fatal("durable event frame", block.String())
	}
	response.Body.Close()
	response = open(fmt.Sprint(cursor))
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	response.Body.Close()
	if err != nil || line != ": heartbeat\n" {
		t.Fatal("resume replayed event", line, err)
	}
	response = open("invalid")
	response.Body.Close()
	if response.StatusCode != 422 {
		t.Fatal("invalid resume cursor", response.StatusCode)
	}
	// Resume past the visible event: the other principal's event is filtered.
	response = open(fmt.Sprint(cursor))
	defer response.Body.Close()
	liveReader := bufio.NewReader(response.Body)
	line, err = liveReader.ReadString('\n')
	if err != nil || line != ": heartbeat\n" {
		t.Fatal("principal visibility", line, err)
	}
	if _, err = db.Exec(ctx, `UPDATE photo.sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, actor); err != nil {
		t.Fatal(err)
	}
	// Queue an event after revocation. Reauthentication must terminate the open
	// stream before the next query can deliver the newly visible payload.
	var revokedCursor int64
	if err = db.QueryRow(ctx, `UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=$1 RETURNING last_cursor`, tenant).Scan(&revokedCursor); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO photo.run_events(tenant_id,cursor,principal_id,audience,type,payload) VALUES($1,$2,$3,'principal','capture.alert','{"private":"revoked-session"}')`, tenant, revokedCursor, actor); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(liveReader)
	if err != nil {
		t.Fatal("stream did not close cleanly after revoke", err)
	}
	if strings.Contains(string(rest), "data:") {
		t.Fatal("revoked session received event", string(rest))
	}
	response = open(fmt.Sprint(cursor))
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("revoked reconnect accepted", response.StatusCode)
	}

}
