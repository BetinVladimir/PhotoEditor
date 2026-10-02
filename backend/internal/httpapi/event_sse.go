package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/eventstream"
)

// Bounded streams fit the HTTP server's write timeout. Clients reconnect with
// their last durable cursor. No transaction or connection is held while idle.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request, events eventstream.Service, who auth.Identity, tenant uuid.UUID, limit int) {
	after := r.URL.Query().Get("after")
	if after == "" {
		after = r.Header.Get("Last-Event-ID")
	}
	page, err := events.List(r.Context(), who, tenant, after, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	writePage := func(page eventstream.Page) bool {
		for _, event := range page.Items {
			body, e := json.Marshal(event)
			if e != nil {
				return false
			}
			if _, e = fmt.Fprintf(w, "id: %s\nevent: message\ndata: %s\n\n", event.Cursor, body); e != nil {
				return false
			}
			after = event.Cursor
		}
		if _, e := fmt.Fprint(w, ": heartbeat\n\n"); e != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !writePage(page) {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			// Recheck signed-token session and tenant/project event visibility each poll.
			current, currentTenant, e := s.tenantRequest(r, false)
			if e != nil || currentTenant != tenant {
				return
			}
			page, e = events.List(r.Context(), current, tenant, after, limit)
			if e != nil {
				return
			}
			if !writePage(page) {
				return
			}
		}
	}
}
